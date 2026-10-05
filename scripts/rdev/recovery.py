"""Private Terraform execution and owner-encrypted recovery artifacts."""
from contextlib import contextmanager
from io import BytesIO
import base64
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import zipfile
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.hazmat.primitives.ciphers.aead import AESGCM


def recipient(path):
    key = serialization.load_pem_public_key(Path(path).read_bytes())
    if not isinstance(key, rsa.RSAPublicKey) or key.key_size < 3072:
        raise ValueError('Recovery requires the reviewed RSA3072 public key')
    return key


def seal_workdir(workdir, output, key, source_sha):
    payload = BytesIO()
    with zipfile.ZipFile(payload, 'w', zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(Path(workdir).rglob('*')):
            if path.is_file() and not path.is_symlink() and ('.tfstate' in path.name or path.suffix == '.log'):
                archive.write(path, path.relative_to(workdir).as_posix())
    aes_key = AESGCM.generate_key(bit_length=256)
    nonce = os.urandom(12)
    encrypted_key = key.encrypt(aes_key, padding.OAEP(mgf=padding.MGF1(hashes.SHA256()), algorithm=hashes.SHA256(), label=None))
    aad = ('raptor-rdev-recovery-v1:' + source_sha).encode()
    envelope = {'version': 1, 'source_sha': source_sha,
                'recipient_sha256': hashlib.sha256(key.public_bytes(serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo)).hexdigest(),
                'wrapped_key': base64.b64encode(encrypted_key).decode(), 'nonce': base64.b64encode(nonce).decode(),
                'ciphertext': base64.b64encode(AESGCM(aes_key).encrypt(nonce, payload.getvalue(), aad)).decode()}
    output = Path(output); output.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = output / 'recovery.json'
    path.write_text(json.dumps(envelope)); path.chmod(0o600)


def decrypt_bundle(path, private_key, output):
    envelope = json.loads(Path(path).read_text())
    if envelope['version'] != 1:
        raise ValueError('Unsupported recovery format')
    key = private_key.decrypt(base64.b64decode(envelope['wrapped_key']), padding.OAEP(mgf=padding.MGF1(hashes.SHA256()), algorithm=hashes.SHA256(), label=None))
    aad = ('raptor-rdev-recovery-v1:' + envelope['source_sha']).encode()
    raw = AESGCM(key).decrypt(base64.b64decode(envelope['nonce']), base64.b64decode(envelope['ciphertext']), aad)
    output = Path(output).resolve(); output.mkdir(mode=0o700, parents=True, exist_ok=True)
    with zipfile.ZipFile(BytesIO(raw)) as archive:
        for entry in archive.infolist():
            target = (output / entry.filename).resolve()
            if entry.is_dir() or not target.is_relative_to(output) or target == output:
                raise ValueError('Unsafe recovery entry')
        for entry in archive.infolist():
            target = (output / entry.filename).resolve()
            target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            target.write_bytes(archive.read(entry)); target.chmod(0o600)


@contextmanager
def durable_workdir(public_key, output, source_sha, parent=None):
    key = recipient(public_key)  # Fail before provisioning if no usable recipient exists.
    root = Path(tempfile.mkdtemp(prefix='rdev-live-plan-', dir=parent)); root.chmod(0o700)
    envfile = os.environ.get('GITHUB_ENV')
    if envfile:
        with Path(envfile).open('a') as stream:
            stream.write('RDEV_WORK_DIRECTORY=' + str(root) + '\n')
    try:
        yield str(root)
    finally:
        # Keep plaintext private until the subsequent encrypted-artifact upload succeeds.
        # In particular, never delete errored.tfstate merely because apply failed.
        seal_workdir(root, output, key, source_sha)


class TerraformRunner:
    def __init__(self, directory, popen=subprocess.Popen, clock=time.monotonic):
        self.directory = Path(directory)
        self.popen = popen
        self.clock = clock
        self.deadline = clock() + 2700  # 45 minutes, leaving 10 minutes of the job for recovery/upload.

    def run(self, args, timeout=180, executable=None):
        remaining = self.deadline - self.clock()
        if args[0] == 'apply' and remaining < 1500:
            raise ValueError('Insufficient execution budget before apply')
        soft_limit = min(timeout, remaining - 300)
        if soft_limit <= 0:
            raise ValueError('Execution budget exhausted before command')
        stdout_path = self.directory / (args[0] + '.stdout.log')
        stderr_path = self.directory / (args[0] + '.stderr.log')
        with stdout_path.open('w') as stdout, stderr_path.open('w') as stderr:
            command = executable if executable else ['terraform', '-chdir=' + str(self.directory)] + args
            process = self.popen(command, stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr)
            try:
                code = process.wait(timeout=soft_limit)
            except subprocess.TimeoutExpired:
                process.send_signal(signal.SIGINT)
                try:
                    process.wait(timeout=120)
                except subprocess.TimeoutExpired:
                    envfile = os.environ.get('GITHUB_ENV')
                    if envfile:
                        with Path(envfile).open('a') as stream:
                            stream.write('RDEV_TERRAFORM_STILL_STOPPING=true\n')
                    # Do not forcibly kill Terraform or remove its working files.
                    # Recovery is a snapshot; reconcile cloud inventory before any retry.
                raise ValueError('Terraform exceeded execution budget; graceful stop requested, no retry') from None
        if code:
            raise ValueError('Terraform failed at ' + args[0] + '; raw diagnostics withheld')
        return stdout_path.read_text()
