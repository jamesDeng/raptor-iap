"""Official SDK boundary. No cloud request occurs at module import."""
import re
import time
import os
import hmac


class StorageError(Exception):
    """Only static public diagnostic codes cross this boundary."""


class StorageAPI:
    def __init__(self, config, nas, volumes, identity, consumer_checker=None):
        from alibabacloud_nas20170626 import models as n
        from alibabacloud_fcsandbox20260509 import models as f
        self.cfg, self.nas, self.volumes, self.identity = config, nas, volumes, identity
        self.n, self.f = n, f
        self.consumer_checker = consumer_checker
        self.owner_nonce = None
        self.filesystem_id = None
        if "agentic_space_id" not in n.CreateAccessPointRequest.__init__.__annotations__:
            raise StorageError("UnsupportedSDKSchema")

    @classmethod
    def from_credentials(cls, config):
        from alibabacloud_credentials.client import Client as Credentials
        from alibabacloud_tea_openapi.models import Config
        from alibabacloud_nas20170626.client import Client as NAS
        from alibabacloud_fcsandbox20260509.client import Client as Volumes
        from alibabacloud_sts20150401.client import Client as STS
        credential = Credentials()
        def options(endpoint):
            return Config(credential=credential, region_id=config.region, endpoint=endpoint,
                          read_timeout=30000, connect_timeout=10000)
        return cls(config, NAS(options("nas.ap-southeast-1.aliyuncs.com")),
                   Volumes(options("fcsandbox.ap-southeast-1.aliyuncs.com")),
                   STS(options("sts.ap-southeast-1.aliyuncs.com")))

    def _call(self, client, method, *args):
        try:
            result = getattr(client, method)(*args)
            body = result.body.to_map()
            if not isinstance(body, dict):
                raise ValueError()
            code = body.get("code")
            if code is not None and str(code) not in ("200", "OK", "Success"):
                raise ValueError()
            return body
        except Exception as error:
            if getattr(error, "code", None) in {
                    "InvalidFileSystem.NotFound", "InvalidAccessPoint.NotFound",
                    "InvalidAgenticSpace.NotFound", "VolumeNotFound"}:
                raise StorageError("ResourceNotFound") from None
            # Raw SDK messages may include request headers or credentials.
            raise StorageError("SDKRequestFailed") from None

    def assert_identity(self):
        body = self._call(self.identity, "get_caller_identity")
        if body.get("AccountId") != self.cfg.account_id:
            raise StorageError("AccountMismatch")

    def _tag(self, name):
        return [getattr(self.n, name)(key="raptor-owner", value=self.owner_nonce)]

    def create_filesystem(self, client_token):
        body = self._call(self.nas, "create_file_system", self.n.CreateFileSystemRequest(
            file_system_type="standard", storage_type="Agentic", protocol_type="NFS",
            charge_type="PayAsYouGo", client_token=client_token,
            description="raptor-" + self.owner_nonce,
            tag=self._tag("CreateFileSystemRequestTag")))
        return self._id(body.get("FileSystemId"))

    @staticmethod
    def _id(value):
        if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9-]{1,128}", value):
            raise StorageError("InvalidSDKResponse")
        return value

    def get_filesystem(self, id):
        self.filesystem_id = id
        result = self._call(self.nas, "describe_file_systems", self.n.DescribeFileSystemsRequest(file_system_id=id))
        rows = result.get("FileSystems", {}).get("FileSystem", [])
        if result.get("TotalCount") == 0 and not rows:
            raise StorageError("ResourceNotFound")
        if len(rows) != 1 or rows[0].get("FileSystemId") != id:
            raise StorageError("ResourceMismatch")
        return rows[0]

    def create_space(self, filesystem_id, client_token):
        body = self._call(self.nas, "create_agentic_space", self.n.CreateAgenticSpaceRequest(
            file_system_id=filesystem_id, azone=self.cfg.zone,
            file_system_path="/raptor-" + self.cfg.run_id,
            description="raptor-" + self.owner_nonce, client_token=client_token,
            quota=self.n.CreateAgenticSpaceRequestQuota(size_limit=10737418240, file_count_limit=10000)))
        return self._id(body.get("AgenticSpaceId"))

    def get_space(self, filesystem_id, space_id):
        return self._call(self.nas, "get_agentic_space", self.n.GetAgenticSpaceRequest(
            file_system_id=filesystem_id, agentic_space_id=space_id)).get("AgenticSpace", {})

    def create_access_point(self, filesystem_id, space_id):
        body = self._call(self.nas, "create_access_point", self.n.CreateAccessPointRequest(
            file_system_id=filesystem_id, agentic_space_id=space_id,
            vpc_id=self.cfg.vpc_id, vsw_id=self.cfg.vswitch_id, enabled_ram=True,
            access_point_name="raptor-" + self.owner_nonce,
            tag=self._tag("CreateAccessPointRequestTag"))).get("AccessPoint", {})
        return self._id(body.get("AccessPointId")), self.valid_domain(body.get("AccessPointDomain"))

    @staticmethod
    def valid_domain(value):
        if not isinstance(value, str) or not re.fullmatch(r"ap-[a-z0-9-]+\.[a-z0-9-]+\.ap-southeast-1\.nas\.aliyuncs\.com", value):
            raise StorageError("InvalidAccessPointDomain")
        return value

    def get_access_point(self, id):
        return self._call(self.nas, "describe_access_point", self.n.DescribeAccessPointRequest(
            file_system_id=self.filesystem_id, access_point_id=id)).get("AccessPoint", {})

    def create_volume(self, name, server_addr, uid, gid):
        body = self._call(self.volumes, "create_volume", self.f.CreateVolumeRequest(
            body=self.f.CreateVolumeInput(team_id=self.cfg.team_id, volume_name=name,
                agentic_fsvolume_config=self.f.AgenticFSVolumeConfig(server_addr=server_addr,
                    user_id=uid, group_id=gid)))).get("volume", {})
        return self._id(body.get("volumeID"))

    def get_volume(self, id):
        return self._call(self.volumes, "get_volume", id,
                          self.f.GetVolumeRequest(team_id=self.cfg.team_id)).get("volume", {})

    def wait_ready(self, getter, status):
        deadline = time.monotonic() + 120
        while True:
            result = getter()
            if str(result.get("Status", "")).lower() == status.lower():
                return result
            if time.monotonic() >= deadline:
                raise StorageError("ReadinessTimeout")
            time.sleep(5)

    def find_pending(self, stage, inventory):
        """Names alone never prove ownership; also match a journaled random nonce."""
        if stage == "filesystem":
            rows = self._call(self.nas, "describe_file_systems", self.n.DescribeFileSystemsRequest(
                storage_type="Agentic", page_size=100, page_number=1))
            if rows.get("TotalCount", 0) > 100:
                raise StorageError("ReconciliationNeedsPagination")
            rows = rows.get("FileSystems", {}).get("FileSystem", [])
            return [r for r in rows if self.owns_filesystem(r)]
        if stage == "space":
            body = self._call(self.nas, "describe_agentic_spaces", self.n.DescribeAgenticSpacesRequest(
                file_system_id=inventory["filesystem_id"], max_results=100))
            rows = body.get("AgenticSpaces", {}).get("AgenticSpace", [])
            if body.get("NextToken"):
                raise StorageError("ReconciliationNeedsPagination")
            return [r for r in rows if r.get("Description") == "raptor-" + self.owner_nonce
                    and r.get("FileSystemPath", "").rstrip("/") == "/raptor-" + self.cfg.run_id]
        if stage == "access_point":
            body = self._call(self.nas, "describe_access_points", self.n.DescribeAccessPointsRequest(
                file_system_id=inventory["filesystem_id"], max_results=100))
            if body.get("NextToken"):
                raise StorageError("ReconciliationNeedsPagination")
            return [r for r in body.get("AccessPoints", []) if
                    r.get("AccessPointName") == "raptor-" + self.owner_nonce and
                    r.get("AgenticSpaceId") == inventory["agentic_space_id"] and self.has_owner_tag(r)]
        body = self._call(self.volumes, "list_volumes", self.f.ListVolumesRequest(
            team_id=self.cfg.team_id, volume_name=inventory["volume_name"], max_results=100))
        if body.get("nextToken"):
            raise StorageError("ReconciliationNeedsPagination")
        return [r for r in body.get("volumes", []) if r.get("volumeName") == inventory["volume_name"]
                and r.get("teamID") == self.cfg.team_id and r.get("agenticFSVolumeConfig", {}).get("serverAddr")
                == inventory["access_point_domain"] + ":/"]

    def has_owner_tag(self, row):
        tags = row.get("Tags", [])
        if isinstance(tags, dict):
            tags = tags.get("Tag", [])
        return any(t.get("Key") == "raptor-owner" and t.get("Value") == self.owner_nonce for t in tags)

    def owns_filesystem(self, row):
        return (row.get("StorageType") == "Agentic" and row.get("ProtocolType") == "NFS"
                and row.get("Description") == "raptor-" + self.owner_nonce and self.has_owner_tag(row))

    def active_consumers(self):
        if self.consumer_checker is not None:
            return self.consumer_checker()
        # Bind the E2B key to the intended Team using the authenticated POP API.
        key_id, key = os.environ.get("E2B_API_KEY_ID"), os.environ.get("E2B_API_KEY")
        if not key_id or not key:
            raise StorageError("ConsumerCheckUnavailable")
        info = self._call(self.volumes, "describe_api_key", key_id,
                          self.f.DescribeApiKeyRequest()).get("apiKey", {})
        actual = info.get("apiKeyValue")
        if info.get("teamID") != self.cfg.team_id or not isinstance(actual, str) or not hmac.compare_digest(actual, key):
            raise StorageError("ConsumerTeamUnverified")
        try:
            from e2b import Sandbox
            from e2b.sandbox.sandbox_api import SandboxQuery
            from e2b.api.client.models.sandbox_state import SandboxState
            pager = Sandbox.list(query=SandboxQuery(state=[SandboxState.RUNNING, SandboxState.PAUSED]),
                                 api_key=key, domain="ap-southeast-1.sandbox.aliyuncs.com",
                                 api_url="https://api.ap-southeast-1.sandbox.aliyuncs.com",
                                 request_timeout=30)
            ids = []
            while pager.has_next:
                ids.extend(item.sandbox_id for item in pager.next_items())
                if ids:
                    return ids  # Any sandbox in the Team blocks cleanup conservatively.
            return []
        except Exception:
            raise StorageError("ConsumerCheckUnavailable") from None

    def space_is_empty(self, filesystem_id, space_id):
        row = self.get_space(filesystem_id, space_id)
        values = [row.get("SpaceUsage"), row.get("FileCountUsage")]
        if any(type(value) is not int or value < 0 for value in values):
            raise StorageError("UnverifiableOccupancy")
        return values == [0, 0]

    def assert_no_foreign_children(self, inventory):
        fsid = inventory.get("filesystem_id")
        if not fsid:
            return
        spaces = self._call(self.nas, "describe_agentic_spaces", self.n.DescribeAgenticSpacesRequest(
            file_system_id=fsid, max_results=100))
        aps = self._call(self.nas, "describe_access_points", self.n.DescribeAccessPointsRequest(
            file_system_id=fsid, max_results=100))
        if spaces.get("NextToken") or aps.get("NextToken"):
            raise StorageError("UnverifiedChildren")
        if any(r.get("AgenticSpaceId") != inventory.get("agentic_space_id")
               for r in spaces.get("AgenticSpaces", {}).get("AgenticSpace", [])):
            raise StorageError("ForeignChildResource")
        if any(r.get("AccessPointId") != inventory.get("access_point_id") for r in aps.get("AccessPoints", [])):
            raise StorageError("ForeignChildResource")

    def delete_volume(self, id):
        self._call(self.volumes, "delete_volume", id, self.f.DeleteVolumeRequest(team_id=self.cfg.team_id))

    def delete_access_point(self, id):
        self._call(self.nas, "delete_access_point", self.n.DeleteAccessPointRequest(
            file_system_id=self.filesystem_id, access_point_id=id))

    def delete_space(self, filesystem_id, space_id):
        self._call(self.nas, "delete_agentic_space", self.n.DeleteAgenticSpaceRequest(
            file_system_id=filesystem_id, agentic_space_id=space_id))

    def delete_filesystem(self, id):
        self._call(self.nas, "delete_file_system", self.n.DeleteFileSystemRequest(file_system_id=id))

    def wait_deleted(self, getter):
        deadline = time.monotonic() + 120
        while True:
            try:
                getter()
            except StorageError as error:
                if str(error) == "ResourceNotFound":
                    return
                raise
            if time.monotonic() >= deadline:
                raise StorageError("DeletionUnconfirmed")
            time.sleep(5)
