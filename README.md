mwarchiver
==========

A minimal MediaWiki archiver. This program is intended to export articles from a MediaWiki instance to a SQLite database.

## Releases notice

This project's GitHub Releases are weekly archives of [52Poké Wiki](https://wiki.52poke.com), licensed under [CC BY-NC-SA 3.0](https://creativecommons.org/licenses/by-nc-sa/3.0/). They are database archives created with mwarchiver, **not binaries of mwarchiver**. Please review the [52Poké Wiki machine reading rules](https://wiki.52poke.com/wiki/%E7%A5%9E%E5%A5%87%E5%AE%9D%E8%B4%9D%E7%99%BE%E7%A7%91:%E6%9C%BA%E5%99%A8%E8%AF%BB%E5%8F%96%E5%AE%88%E5%88%99).

## Configuration

By default the program will load `$HOME/.mwarchiver.yaml` if the file exists. You can also specify a config file with `--config`.

Example config:

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

mwarchiver is licensed under the [MIT](LICENSE) license.
