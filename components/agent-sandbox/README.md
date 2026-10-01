# Pi sandbox image

The image provides Pi `@earendil-works/pi-coding-agent@0.99.2`, Bash, Git, curl and CA certificates, with `/workspace` as its working directory. The default command is `sleep infinity`. It retains root and writable `/etc/passwd` and `/etc/group` for later Aliyun initialization.

## Pinned inputs

Resolved from public registry metadata on 2026-10-01:

- Official `node:22-bookworm`, Node `22.23.3` (Pi requires Node `>=22.19.0`).
- Multi-platform index: `sha256:363e1587494626837fa7f9a23bdb453d13b0ff3c67c705c2805cfc69c2d2fad7`.
- Linux AMD64 manifest used by the Dockerfile: `sha256:17b7fd60fd812617654c64b95f9b2dde94f103313073b672bc40fdad6dccbaa2`.
- Pi tarball integrity: `sha512-6R1BZ2N77CrVcGf3eC2KovTz1Q4RYiAeydvVWQT546N2fi1nBc81aURlbOZCgruWoW9VY/UrLzDynF4YTolpoA==`.

The [published Pi metadata](https://registry.npmjs.org/@earendil-works/pi-coding-agent/0.99.2) identifies the [tarball](https://registry.npmjs.org/@earendil-works/pi-coding-agent/-/pi-coding-agent-0.99.2.tgz). Its `npm-shrinkwrap.json` uses lockfile version 3 and contains 147 package records. npm uses that published shrinkwrap during installation. Seven first-party records have exact versions and resolved registry tarballs but no integrity field; the remaining dependency records carry integrity. Lifecycle scripts are disabled with `--ignore-scripts`.

The base digest and published package lock constrain inputs. Debian utility packages are fetched from the distribution repositories at build time, so identical source builds are not guaranteed to produce identical image digests. Disabled dependency scripts may limit functionality beyond the CLI startup tested here.

## Build and check

On a Docker host, from the repository root:

```sh
docker build --platform linux/amd64 -t raptor-iap-pi:local components/agent-sandbox
components/agent-sandbox/check-image.sh raptor-iap-pi:local
```

The script creates a real container using the image's default command with networking disabled. It verifies Linux AMD64, continued execution after three seconds, Pi version `0.99.2`, the Node version floor, required utilities, the certificate bundle and writable account files. An EXIT trap removes the container on success or failure. It passes no environment credentials and makes no model or cloud calls.

Local shell syntax checks are available; Docker is absent on the development Mac. Real image build and offline acceptance results must come from the Actions run before describing this image as verified. A passing offline check proves neither Aliyun reachability nor Gen2 execution, model integration, secret retrieval, or operation safety.

The Docker context admits only its Dockerfile and ignore file. Do not add API keys, registry credentials, `.env` files, Pi sessions or model configuration to it.
