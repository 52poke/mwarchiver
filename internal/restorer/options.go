package restorer

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Target string

const (
	TargetNew      Target = "new"
	TargetExisting Target = "existing"
)

type Options struct {
	DumpPath           string
	StateDir           string
	Target             Target
	MediaWikiDir       string
	Image              string
	ContainerCLI       string
	ProjectName        string
	BindAddress        string
	Port               int
	DBHost             string
	DBPort             int
	DBName             string
	DBUser             string
	DBPasswordFile     string
	AdminUser          string
	AdminEmail         string
	Elasticsearch      bool
	ElasticsearchImage string
	EventBusURL        string
	OAuth              bool
	OAuthName          string
	OAuthDescription   string
	OAuthVersion       string
	OAuthCallbackURL   string
	OAuthGrants        []string
	ForceSettings      bool
	SkipSearchIndex    bool
	HostUID            string
	HostGID            string
}

func (o *Options) Normalize() error {
	if o.Target == "" {
		o.Target = TargetNew
	}
	if o.Target != TargetNew && o.Target != TargetExisting {
		return fmt.Errorf("target must be %q or %q", TargetNew, TargetExisting)
	}
	if o.Image == "" {
		o.Image = "ghcr.io/52poke/mediawiki:latest"
	}
	if o.ContainerCLI == "" {
		o.ContainerCLI = "docker"
	}
	if o.ProjectName == "" {
		o.ProjectName = "mwarchiver-restore"
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`).MatchString(o.ProjectName) {
		return fmt.Errorf("invalid Compose project name %q", o.ProjectName)
	}
	if o.BindAddress == "" {
		o.BindAddress = "127.0.0.1"
	}
	ip := net.ParseIP(o.BindAddress)
	if ip == nil {
		return fmt.Errorf("bind address must be an IPv4 or IPv6 address")
	}
	o.BindAddress = ip.String()
	if o.Port == 0 {
		o.Port = 8227
	}
	if o.Port < 1 || o.Port > 65535 {
		return fmt.Errorf("HTTP port must be between 1 and 65535")
	}
	if o.DBPort == 0 {
		o.DBPort = 3306
	}
	if o.DBPort < 1 || o.DBPort > 65535 {
		return fmt.Errorf("database port must be between 1 and 65535")
	}
	if o.DBName == "" {
		o.DBName = "52poke_wiki"
	}
	if o.DBUser == "" {
		o.DBUser = "mediawiki"
	}
	if o.AdminUser == "" {
		o.AdminUser = "Admin"
	}
	if o.AdminEmail == "" {
		o.AdminEmail = "admin@example.invalid"
	}
	if o.ElasticsearchImage == "" {
		o.ElasticsearchImage = "ghcr.io/52poke/52w-elasticsearch:latest"
	}
	if o.OAuthName == "" {
		o.OAuthName = "Local development client"
	}
	if o.OAuthDescription == "" {
		o.OAuthDescription = "OAuth client created by mwarchiver restore"
	}
	if o.OAuthVersion == "" {
		o.OAuthVersion = "1.0"
	}
	if o.OAuthCallbackURL == "" {
		o.OAuthCallbackURL = o.WikiURL() + "/"
	}
	if len(o.OAuthGrants) == 0 {
		o.OAuthGrants = []string{"basic"}
	}

	var err error
	o.DumpPath, err = existingFile(o.DumpPath, "dump")
	if err != nil {
		return err
	}
	if o.StateDir == "" {
		o.StateDir = filepath.Join(".local", "restore")
	}
	o.StateDir, err = filepath.Abs(o.StateDir)
	if err != nil {
		return fmt.Errorf("resolve state directory: %w", err)
	}
	if o.MediaWikiDir != "" {
		o.MediaWikiDir, err = filepath.Abs(o.MediaWikiDir)
		if err != nil {
			return fmt.Errorf("resolve MediaWiki checkout: %w", err)
		}
		for _, path := range []string{
			filepath.Join(o.MediaWikiDir, "maintenance", "run.php"),
			filepath.Join(o.MediaWikiDir, ".devcontainer", "Dockerfile"),
			filepath.Join(o.MediaWikiDir, ".devcontainer", "docker-compose.yml"),
			filepath.Join(o.MediaWikiDir, "docker", "nginx.conf"),
		} {
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("%s is not a usable 52poke/mediawiki checkout: %w", o.MediaWikiDir, err)
			}
		}
		if o.HostUID == "" || o.HostGID == "" {
			currentUser, err := user.Current()
			if err != nil {
				return fmt.Errorf("resolve current user for checkout permissions: %w", err)
			}
			if o.HostUID == "" {
				o.HostUID = currentUser.Uid
			}
			if o.HostGID == "" {
				o.HostGID = currentUser.Gid
			}
		}
		positiveID := regexp.MustCompile(`^[1-9][0-9]*$`)
		if !positiveID.MatchString(o.HostUID) || !positiveID.MatchString(o.HostGID) {
			return fmt.Errorf("checkout restore requires a non-root user with numeric UID and GID")
		}
	}
	if o.Target == TargetNew {
		o.DBHost = "mariadb"
	} else {
		if strings.TrimSpace(o.DBHost) == "" {
			return fmt.Errorf("--db-host is required for an existing installation")
		}
		o.DBPasswordFile, err = existingFile(o.DBPasswordFile, "database password")
		if err != nil {
			return err
		}
	}
	if err := validateHTTPURL(o.EventBusURL, "EventBus endpoint"); err != nil {
		return err
	}
	if o.OAuth {
		if err := validateHTTPURL(o.OAuthCallbackURL, "OAuth callback"); err != nil {
			return err
		}
		if !strings.Contains(o.AdminEmail, "@") {
			return fmt.Errorf("an administrator email is required to create an OAuth client")
		}
	}
	for name, value := range map[string]string{
		"database name": o.DBName,
		"database user": o.DBUser,
		"admin user":    o.AdminUser,
	} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("invalid %s", name)
		}
	}
	return nil
}

func (o Options) WikiURL() string {
	return "http://" + net.JoinHostPort(o.publicHost(), strconv.Itoa(o.Port))
}

func (o Options) PortBinding(hostPort, containerPort int) string {
	return net.JoinHostPort(o.BindAddress, strconv.Itoa(hostPort)) + ":" + strconv.Itoa(containerPort)
}

func (o Options) ExternalHostPort(port int) string {
	return net.JoinHostPort(o.publicHost(), strconv.Itoa(port))
}

func (o Options) publicHost() string {
	ip := net.ParseIP(o.BindAddress)
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return "localhost"
	}
	return ip.String()
}

func existingFile(path, label string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s path is required", label)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s path: %w", label, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("open %s path: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s path is not a regular file: %s", label, abs)
	}
	return abs, nil
}

func validateHTTPURL(value, label string) error {
	if value == "" {
		return nil
	}
	u, err := url.ParseRequestURI(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid %s URL %q", label, value)
	}
	return nil
}
