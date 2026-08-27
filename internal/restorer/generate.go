package restorer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

type Layout struct {
	ComposePath      string
	BaseComposePath  string
	SettingsPath     string
	AdminPassword    string
	OAuthCredentials string
	SettingsBackup   string
	StatePath        string
}

func Prepare(o Options) (Layout, error) {
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		return Layout{}, fmt.Errorf("create state directory: %w", err)
	}
	if err := os.Chmod(o.StateDir, 0o700); err != nil {
		return Layout{}, fmt.Errorf("secure state directory: %w", err)
	}
	for name, bytes := range map[string]int{
		"mariadb-root-password":    32,
		"mariadb-password":         32,
		"mediawiki-admin-password": 24,
		"mediawiki-secret-key":     64,
	} {
		if o.Target == TargetExisting && name == "mariadb-root-password" {
			continue
		}
		path := filepath.Join(o.StateDir, name)
		if err := ensureSecret(path, bytes); err != nil {
			return Layout{}, err
		}
	}
	if o.Target == TargetExisting {
		password, err := os.ReadFile(o.DBPasswordFile)
		if err != nil {
			return Layout{}, fmt.Errorf("read database password: %w", err)
		}
		if err := writeAtomic(filepath.Join(o.StateDir, "mariadb-password"), password, 0o600); err != nil {
			return Layout{}, fmt.Errorf("copy database password: %w", err)
		}
	}

	settingsPath := filepath.Join(o.StateDir, "LocalSettings.php")
	if o.MediaWikiDir != "" {
		settingsPath = filepath.Join(o.MediaWikiDir, "LocalSettings.php")
	}
	settings, err := renderSettings(o)
	if err != nil {
		return Layout{}, err
	}
	settingsBackup := ""
	if o.MediaWikiDir != "" {
		settingsBackup = filepath.Join(o.StateDir, "checkout-LocalSettings.php.backup")
	}
	if err := writeSettings(settingsPath, settings, o.ForceSettings, o.MediaWikiDir != "", settingsBackup); err != nil {
		return Layout{}, err
	}

	compose, err := renderCompose(o, settingsPath)
	if err != nil {
		return Layout{}, err
	}
	composePath := filepath.Join(o.StateDir, "compose.yaml")
	if err := writeAtomic(composePath, compose, 0o600); err != nil {
		return Layout{}, fmt.Errorf("write Compose configuration: %w", err)
	}
	layout := Layout{
		ComposePath: composePath,
		BaseComposePath: func() string {
			if o.MediaWikiDir == "" {
				return ""
			}
			return filepath.Join(o.MediaWikiDir, ".devcontainer", "docker-compose.yml")
		}(),
		SettingsPath:     settingsPath,
		AdminPassword:    filepath.Join(o.StateDir, "mediawiki-admin-password"),
		OAuthCredentials: filepath.Join(o.StateDir, "oauth-client.json"),
		SettingsBackup:   settingsBackup,
		StatePath:        filepath.Join(o.StateDir, "state.json"),
	}
	if err := writeState(layout.StatePath, stateFor(o, layout)); err != nil {
		return Layout{}, err
	}
	return layout, nil
}

func ensureSecret(path string, size int) error {
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("secret is not a regular file: %s", path)
		}
		return os.Chmod(path, 0o600)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect secret %s: %w", path, err)
	}
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generate secret %s: %w", path, err)
	}
	value := make([]byte, hex.EncodedLen(len(raw))+1)
	hex.Encode(value, raw)
	value[len(value)-1] = '\n'
	return writeAtomic(path, value, 0o600)
}

func writeSettings(path string, data []byte, force, checkout bool, backupPath string) error {
	current, err := os.ReadFile(path)
	if err == nil {
		if bytes.Equal(current, data) {
			return nil
		}
		if checkout && !force {
			return fmt.Errorf("%s already exists; inspect it and rerun with --force-settings to replace it", path)
		}
		if checkout {
			if _, backupErr := os.Stat(backupPath); os.IsNotExist(backupErr) {
				if err := writeAtomic(backupPath, current, 0o600); err != nil {
					return fmt.Errorf("back up existing LocalSettings.php: %w", err)
				}
			} else if backupErr != nil {
				return fmt.Errorf("inspect LocalSettings.php backup: %w", backupErr)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing LocalSettings.php: %w", err)
	}
	return writeAtomic(path, data, 0o644)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mwarchiver-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func renderSettings(o Options) ([]byte, error) {
	t, err := template.New("LocalSettings.php").Funcs(template.FuncMap{
		"php": phpQuote,
	}).Parse(localSettingsTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse LocalSettings template: %w", err)
	}
	var output bytes.Buffer
	if err := t.Execute(&output, o); err != nil {
		return nil, fmt.Errorf("render LocalSettings.php: %w", err)
	}
	return output.Bytes(), nil
}

func renderCompose(o Options, settingsPath string) ([]byte, error) {
	data := struct {
		Options
		SettingsPath string
		DumpDir      string
	}{o, settingsPath, filepath.Dir(o.DumpPath)}
	t, err := template.New("compose.yaml").Funcs(template.FuncMap{
		"yaml": strconv.Quote,
	}).Parse(composeTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse Compose template: %w", err)
	}
	var output bytes.Buffer
	if err := t.Execute(&output, data); err != nil {
		return nil, fmt.Errorf("render Compose configuration: %w", err)
	}
	return output.Bytes(), nil
}

func phpQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return `'` + value + `'`
}

const localSettingsTemplate = `<?php

// Generated by mwarchiver restore. Local-only: no production credentials.
$restoreSecret = static function ( string $name ): string {
	$value = file_get_contents( '/run/secrets/' . $name );
	if ( $value === false ) {
		throw new RuntimeException( 'Cannot read restore secret: ' . $name );
	}
	return trim( $value );
};

$wgSitename = '神奇宝贝百科';
$wgMetaNamespace = '神奇宝贝百科';
$wgLanguageCode = 'zh';
$wgServer = {{ php (printf "http://localhost:%d" .Port) }};
$wgCanonicalServer = $wgServer;
$wgScriptPath = '';
$wgArticlePath = '/wiki/$1';
$wgUsePathInfo = true;

$wgDBtype = 'mysql';
$wgDBserver = {{ php .DBHost }};
$wgDBport = {{ .DBPort }};
$wgDBname = {{ php .DBName }};
$wgDBuser = {{ php .DBUser }};
$wgDBpassword = $restoreSecret( 'mariadb-password' );
$wgDBprefix = '';
$wgDBTableOptions = 'ENGINE=InnoDB, DEFAULT CHARSET=binary';

$wgSecretKey = $restoreSecret( 'mediawiki-secret-key' );
$wgUpgradeKey = substr( hash( 'sha256', $wgSecretKey ), 0, 16 );
$wgEnableEmail = false;
$wgEnableUploads = false;
$wgAllowSchemaUpdates = true;
$wgShowExceptionDetails = true;
$wgDevelopmentWarnings = true;
$wgCookieSecure = false;
$wgSecureLogin = false;
$wgCacheDirectory = sys_get_temp_dir() . '/mediawiki-cache';

$wgDefaultSkin = 'vector-2022';
foreach ( [ 'Vector', 'MinervaNeue', 'MonoBook' ] as $skin ) {
	if ( is_file( "$IP/skins/$skin/skin.json" ) ) {
		wfLoadSkin( $skin );
	}
}

$localExtensions = [
	'CategoryTree', 'Cite', 'Gadgets', 'ImageMap', 'InputBox', 'Interwiki',
	'Math', 'ParserFunctions', 'Poem', 'RSS', 'SyntaxHighlight_GeSHi',
	'TemplateData', 'TemplateStyles',
];
foreach ( $localExtensions as $extension ) {
	if ( is_file( "$IP/extensions/$extension/extension.json" ) ) {
		wfLoadExtension( $extension );
	}
}
{{ if .MediaWikiDir }}
$wgMainCacheType = CACHE_MEMCACHED;
$wgMemCachedServers = [ 'memcached:11211' ];
{{ else }}
$wgMainCacheType = CACHE_NONE;
{{ end }}
{{ if .Elasticsearch }}
if ( !is_file( "$IP/extensions/Elastica/extension.json" ) || !is_file( "$IP/extensions/CirrusSearch/extension.json" ) ) {
	throw new RuntimeException( 'Elasticsearch was requested but Elastica or CirrusSearch is unavailable' );
}
wfLoadExtension( 'Elastica' );
wfLoadExtension( 'CirrusSearch' );
$wgCirrusSearchServers = [ 'default' => 'elasticsearch' ];
$wgSearchType = 'CirrusSearch';
$wgCirrusSearchEnableIncomingLinkCounting = false;
{{ end }}
{{ if .OAuth }}
if ( !is_file( "$IP/extensions/OAuth/extension.json" ) ) {
	throw new RuntimeException( 'OAuth was requested but the extension is unavailable' );
}
wfLoadExtension( 'OAuth' );
$wgGroupPermissions['sysop']['mwoauthproposeconsumer'] = true;
$wgGroupPermissions['sysop']['mwoauthupdateownconsumer'] = true;
$wgGroupPermissions['sysop']['mwoauthmanageconsumer'] = true;
$wgGroupPermissions['sysop']['mwoauthviewprivate'] = true;
{{ end }}
{{ if .EventBusURL }}
if ( !is_file( "$IP/extensions/EventBus/extension.json" ) ) {
	throw new RuntimeException( 'EventBus was requested but the extension is unavailable' );
}
wfLoadExtension( 'EventBus' );
$wgEnableEventBus = 'TYPE_ALL';
$wgEventServices = [
	'eventbus' => [ 'url' => {{ php .EventBusURL }}, 'timeout' => 10 ],
];
$wgEventServiceDefault = 'eventbus';
$wgEventRelayerConfig = [
	'cdn-url-purges' => [
		'class' => \MediaWiki\Extension\EventBus\Adapters\EventRelayer\CdnPurgeEventRelayer::class,
		'stream' => 'cdn-url-purges',
	],
	'default' => [
		'class' => \Wikimedia\EventRelayer\EventRelayerNull::class,
	],
];
$wgJobRunRate = 0;
$wgJobTypeConf['default'] = [
	'class' => '\\MediaWiki\\Extension\\EventBus\\Adapters\\JobQueue\\JobQueueEventBus',
	'readOnlyReason' => false,
];
{{ end }}

$wgExtraNamespaces[100] = '附录';
$wgExtraNamespaces[101] = '附录_talk';
$wgExtraNamespaces[102] = 'Pre';
$wgExtraNamespaces[103] = 'Pre_talk';
$wgNamespaceAliases['52W'] = NS_PROJECT;
$wgNamespaceAliases['神奇寶貝百科'] = NS_PROJECT;
$wgNonincludableNamespaces[] = 102;
$wgNonincludableNamespaces[] = 103;

$wgEnableCreativeCommonsRdf = true;
$wgRightsPage = '神奇宝贝百科:版权声明';
$wgRightsUrl = 'https://creativecommons.org/licenses/by-nc-sa/3.0/deed.zh';
$wgRightsText = '署名-非商业性使用-相同方式共享 3.0';
$wgGroupPermissions['*']['createaccount'] = false;
$wgGroupPermissions['*']['createpage'] = false;
$wgGroupPermissions['*']['edit'] = false;
$wgGroupPermissions['*']['writeapi'] = false;
$wgGroupPermissions['user']['createpage'] = true;
$wgGroupPermissions['user']['edit'] = true;
$wgAllowExternalImagesFrom = [
	'https://wiki.52poke.com/',
	'https://media.52poke.com/',
];
`

const composeTemplate = `services:
{{ if eq .Target "new" }}
  mariadb:
    image: docker.io/library/mariadb:11.4
    command: ["--character-set-server=utf8mb4", "--collation-server=utf8mb4_bin"]
    environment:
      MARIADB_DATABASE: {{ yaml .DBName }}
      MARIADB_USER: {{ yaml .DBUser }}
      MARIADB_PASSWORD_FILE: /run/secrets/mariadb-password
      MARIADB_ROOT_PASSWORD_FILE: /run/secrets/mariadb-root-password
    healthcheck:
      test: ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]
      interval: 2s
      timeout: 5s
      retries: 60
      start_period: 10s
    restart: unless-stopped
    secrets:
      - mariadb-password
      - mariadb-root-password
    volumes:
      - mariadb-data:/var/lib/mysql
{{ end }}
{{ if .MediaWikiDir }}
  wiki-52poke:
    build:
      args:
        USER_ID: {{ yaml .HostUID }}
        GROUP_ID: {{ yaml .HostGID }}
    environment:
      HOME: /home/www-data
      COMPOSER_HOME: /home/www-data/.composer
      MW_RESTORE: "1"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    ports: !override
      - "127.0.0.1:{{ .Port }}:80"
    secrets:
      - mariadb-password
      - mediawiki-admin-password
      - mediawiki-secret-key
    volumes:
      - {{ yaml (printf "%s:/restore/input:ro,z" .DumpDir) }}
    depends_on: !override
      memcached:
        condition: service_started
{{ if .Elasticsearch }}
      elasticsearch:
        condition: service_healthy
{{ end }}
{{ if eq .Target "new" }}
      mariadb:
        condition: service_healthy
{{ end }}
{{ else }}
  mediawiki:
    image: {{ yaml .Image }}
    extra_hosts:
      - "host.docker.internal:host-gateway"
    ports:
      - "127.0.0.1:{{ .Port }}:80"
    restart: unless-stopped
    secrets:
      - mariadb-password
      - mediawiki-secret-key
    volumes:
      - {{ yaml (printf "%s:/home/52poke/wiki/LocalSettings.php:ro,z" .SettingsPath) }}
{{ if or (eq .Target "new") .Elasticsearch }}
    depends_on:
{{ if eq .Target "new" }}
      mariadb:
        condition: service_healthy
{{ end }}
{{ if .Elasticsearch }}
      elasticsearch:
        condition: service_healthy
{{ end }}
{{ end }}
  maintenance:
    image: {{ yaml .Image }}
    working_dir: /home/52poke/wiki
    extra_hosts:
      - "host.docker.internal:host-gateway"
    secrets:
      - mariadb-password
      - mediawiki-admin-password
      - mediawiki-secret-key
    volumes:
      - {{ yaml (printf "%s:/home/52poke/wiki/LocalSettings.php:ro,z" .SettingsPath) }}
      - {{ yaml (printf "%s:/restore/input:ro,z" .DumpDir) }}
{{ end }}
{{ if .Elasticsearch }}
  elasticsearch:
    image: {{ yaml .ElasticsearchImage }}
    environment:
      ES_JAVA_OPTS: -Xmx384m -Xms384m
      discovery.type: single-node
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:9200"]
      interval: 2s
      timeout: 5s
      retries: 60
      start_period: 10s
    restart: unless-stopped
    volumes:
      - elasticsearch-data:/usr/share/elasticsearch/data
{{ end }}
{{ if .EventBusURL }}
  kafka:
    image: docker.io/bitnamilegacy/kafka:3.8.0
    ports:
      - "127.0.0.1:9092:29092"
    environment:
      KAFKA_HEAP_OPTS: -Xmx256m -Xms256m
      KAFKA_ENABLE_KRAFT: "yes"
      KAFKA_CFG_PROCESS_ROLES: broker,controller
      KAFKA_CFG_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_CFG_LISTENERS: INTERNAL://:9092,EXTERNAL://:29092,CONTROLLER://:9093
      KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP: CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT
      KAFKA_CFG_INTER_BROKER_LISTENER_NAME: INTERNAL
      KAFKA_CFG_NODE_ID: "1"
      KAFKA_CFG_CONTROLLER_QUORUM_VOTERS: 1@127.0.0.1:9093
      KAFKA_CFG_ADVERTISED_LISTENERS: INTERNAL://kafka:9092,EXTERNAL://localhost:9092
      KAFKA_CFG_LOG_RETENTION_HOURS: "24"
      ALLOW_PLAINTEXT_LISTENER: "yes"
    healthcheck:
      test: ["CMD", "/opt/bitnami/kafka/bin/kafka-topics.sh", "--bootstrap-server", "localhost:9092", "--list"]
      interval: 2s
      timeout: 5s
      retries: 60
      start_period: 10s
    restart: unless-stopped
    volumes:
      - kafka-data:/bitnami/kafka
{{ end }}

{{ if or (eq .Target "new") .Elasticsearch .EventBusURL }}
volumes:
{{ if eq .Target "new" }}  mariadb-data:
{{ end }}{{ if .Elasticsearch }}  elasticsearch-data:
{{ end }}{{ if .EventBusURL }}  kafka-data:
{{ end }}{{ end }}
secrets:
  mariadb-password:
    file: {{ yaml (printf "%s/mariadb-password" .StateDir) }}
  mediawiki-admin-password:
    file: {{ yaml (printf "%s/mediawiki-admin-password" .StateDir) }}
  mediawiki-secret-key:
    file: {{ yaml (printf "%s/mediawiki-secret-key" .StateDir) }}
{{ if eq .Target "new" }}
  mariadb-root-password:
    file: {{ yaml (printf "%s/mariadb-root-password" .StateDir) }}
{{ end }}`
