mwarchiver
==========

A minimal MediaWiki archiver. This program is intended to export articles from a MediaWiki instance to a SQLite database.

## Configuration

This program can be configured with `$HOME/.mwarchiver.yaml` file:

```yaml
api_url: https://en.wikipedia.org/w/api.php
user_agent: "mwarchiver (contact: you@example.com)"
db_path: mwarchiver.db
limit: 1000 # Maximum exports of a single namespace
namespaces: [0] # List of namespace numbers: https://www.mediawiki.org/wiki/Help:Namespaces
```

Environment variables (prefix `MWARCHIVER_`):

- `MWARCHIVER_API_URL`
- `MWARCHIVER_USER_AGENT`
- `MWARCHIVER_DB_PATH`
- `MWARCHIVER_OUTPUT_PATH`
- `MWARCHIVER_LIMIT`
- `MWARCHIVER_NAMESPACES` (comma-separated, e.g. `0,1,2`)

## Docker usage

Run with a mounted config file:

```bash
docker run --rm \
  -v "$PWD/.mwarchiver.yaml:/root/.mwarchiver.yaml:ro" \
  -v "$PWD:/data" \
  -w /data \
  ghcr.io/52poke/mwarchiver:latest
```

Run with env vars instead of a config file:

```bash
docker run --rm \
  -e MWARCHIVER_API_URL=https://en.wikipedia.org/w/api.php \
  -e MWARCHIVER_DB_PATH=/data/mwarchiver.db \
  -e MWARCHIVER_LIMIT=1000 \
  -e MWARCHIVER_NAMESPACES=0 \
  -v "$PWD:/data" \
  -w /data \
  ghcr.io/52poke/mwarchiver:latest
```

Optional release upload (GitHub Releases):

```bash
docker run --rm \
  -e MWARCHIVER_API_URL=https://en.wikipedia.org/w/api.php \
  -e MWARCHIVER_DB_PATH=/data/mwarchiver.db \
  -e RELEASE_UPLOAD=1 \
  -e GITHUB_TOKEN=ghp_yourtoken \
  -e GITHUB_REPOSITORY=owner/repo \
  -v "$PWD:/data" \
  -w /data \
  ghcr.io/52poke/mwarchiver:latest
```

## Sample Kubernetes CronJob

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: mwarchiver
spec:
  schedule: "0 4 * * 0"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: mwarchiver
              image: ghcr.io/52poke/mwarchiver:latest
              env:
                - name: MWARCHIVER_API_URL
                  value: "https://en.wikipedia.org/w/api.php"
                - name: MWARCHIVER_DB_PATH
                  value: /data/mwarchiver.db
                - name: MWARCHIVER_LIMIT
                  value: "1000"
                - name: MWARCHIVER_NAMESPACES
                  value: "0"
                - name: RELEASE_UPLOAD
                  value: "1"
                - name: GITHUB_REPOSITORY
                  value: "OWNER/REPO"
                - name: GITHUB_TOKEN
                  valueFrom:
                    secretKeyRef:
                      name: mwarchiver-github
                      key: token
              volumeMounts:
                - name: data
                  mountPath: /data
          volumes:
            - name: data
              persistentVolumeClaim:
                claimName: mwarchiver-data
```

## License

[MIT](LICENSE)
