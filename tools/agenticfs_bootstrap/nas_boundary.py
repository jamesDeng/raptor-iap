"""Validate raw NAS collections before generated models supply empty defaults."""
from alibabacloud_nas20170626.client import Client

from .api import StorageAPI, StorageError


def validate_children(action, body):
    contracts = {
        "DescribeAgenticSpaces": ("AgenticSpaces", "AgenticSpaceId", "AgenticSpace"),
        "DescribeAccessPoints": ("AccessPoints", "AccessPointId", None),
    }
    if action in contracts:
        if not isinstance(body, dict):
            raise StorageError("UnverifiedChildren")
        StorageAPI.rows(body, *contracts[action])


class GuardedNAS(Client):
    def call_api(self, params, request, runtime):
        response = super().call_api(params, request, runtime)
        validate_children(params.action, response.get("body"))
        return response
