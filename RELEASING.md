# Release Versioning

## Version bump policy

Every user-facing bug-fix or feature delivery batch increments the SemVer patch
version exactly once before handoff. Multiple commits that belong to the same
delivery batch share that one bump. Documentation-only, test-only, and internal
maintenance changes do not require a version bump unless explicitly requested.

## Synchronized version surfaces

Update all of these files to the same version:

- `VERSION`
- `internal/version/version.go`
- `web/package.json`
- the top-level and root-package versions in `web/package-lock.json`
- the version badges in `README.md` and `README.en.md`
- the newest `CHANGELOG.md` release heading
- the default `MIZUPANEL_IMAGE` references in `docker-compose.yml` and
  `docker-compose.mysql.yml`

Add release notes as a new topmost `## v<version> - YYYY-MM-DD` section. Never
rename a historical release section to describe new work.

## Docker Hub credentials

The release workflow reads these repository-level GitHub Actions Secrets:

- `DOCKERHUB_USERNAME`: the Docker Hub account that can publish
  `leokon3/mizupanel`
- `DOCKERHUB_TOKEN`: a Docker Hub access token with read/write access to that
  repository

The token and all other secret values belong only in GitHub Actions Secrets.
Never place them in repository files, Compose files, application databases,
frontend settings, workflow commands, public documentation examples, or logs.
The production image repository is public, so deployment hosts do not log in to
Docker Hub to pull it.

## Verification

Run the synchronization guard and release gates before creating the tag:

```bash
go test ./internal/version
go test ./...
npm test --prefix web
npm run build --prefix web
docker compose config --quiet
MIZUPANEL_MYSQL_DATABASE=mizupanel \
MIZUPANEL_MYSQL_USERNAME=mizupanel \
MIZUPANEL_MYSQL_PASSWORD=test-only \
MIZUPANEL_MYSQL_ROOT_PASSWORD=test-only-root \
docker compose -f docker-compose.mysql.yml config --quiet
```

Release tags must be exactly `v<VERSION>`. A tag push or an explicit manual
dispatch with that existing tag starts the workflow; ordinary branch pushes do
not publish packages or images. Verification rejects a same-named branch or a
tag that does not point at the checked-out commit. Publication jobs then check
out the verified commit SHA, so moving a tag cannot change their build input.

## Published artifacts

After the shared Go/frontend/version verification job succeeds, the workflow
runs two independent publication jobs:

1. Build and upload the existing Linux amd64 and arm64 GitHub Release packages.
2. Build one Docker Buildx manifest for `linux/amd64` and `linux/arm64`, then
   publish `leokon3/mizupanel:<VERSION>`. A formal tag-push run also updates
   `leokon3/mizupanel:latest`; manual dispatches publish only the immutable
   SemVer tag so rerunning an older release cannot move `latest` backwards.

The SemVer image tag is the production, upgrade, and rollback reference.
`latest` is only a convenience pointer. Never move a release Git tag or reuse a
SemVer image tag for different source. Buildx must finish both architectures
before it publishes the formal manifest; do not manually publish a single-arch
replacement under a release tag.

Verify the public image after the workflow completes:

```bash
VERSION="$(tr -d '\r\n ' < VERSION)"
docker buildx imagetools inspect "leokon3/mizupanel:${VERSION}"
docker buildx imagetools inspect leokon3/mizupanel:latest
```

The versioned manifest must list both `linux/amd64` and `linux/arm64`. Confirm
that the GitHub Release contains both tarballs and that the image labels report
the release source, tag revision, version, and creation time.

## Reruns and failures

The release workflow is safe to rerun for the same unchanged tag. GitHub Release
assets are overwritten by name, and the immutable Docker tag is republished
from the same validated source. Use the manual workflow input to repair that
specific SemVer image. To repair `latest`, rerun the original tag-push workflow;
never move a release tag or use an older manual release to update `latest`.

If package publication fails, rerun after fixing the workflow or transient
service failure. If an architecture build or Docker login fails, Buildx does
not publish a successful single-architecture formal manifest; rerun the full
image job after correcting the issue. A failed application upgrade does not
remove `/app/data` or the MySQL volume—operators should select the prior SemVer
image and recreate the container using the documented rollback flow.
