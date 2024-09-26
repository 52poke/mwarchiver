mwarchiver
==========

A minimal MediaWiki archiver. This program is intended to export articles from a MediaWiki instance to `.txt` files.

## Configruation

This program can be configured with `$HOME/.mwarchiver.yaml` file:

```yaml
api_url: https://en.wikipedia.org/w/api.php
output_path: output
limit: 1000 # Maximum exports of a single namespace
namespaces: [0] # List of namespace numbers: https://www.mediawiki.org/wiki/Help:Namespaces
```

## License

[MIT](LICENSE)