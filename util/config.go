package util

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"

	"io"
	"strings"

	"github.com/gorilla/securecookie"
)

// Cookie is a runtime generated secure cookie used for authentication
var Cookie *securecookie.SecureCookie

// WebHostURL is the public route to the semaphore server
var WebHostURL *url.URL

type DbDriver int

const (
	DbDriverMySQL DbDriver = iota
	DbDriverBolt
	DbDriverPostgres
)

type DbConfig struct {
	Dialect  DbDriver `json:"-"`
	Hostname string   `json:"host"`
	Username string   `json:"user"`
	Password string   `json:"pass"`
	DbName   string   `json:"name"`
}

type ldapMappings struct {
	DN   string `json:"dn"`
	Mail string `json:"mail"`
	UID  string `json:"uid"`
	CN   string `json:"cn"`
}

//ConfigType mapping between Config and the json file that sets it
type ConfigType struct {
	MySQL  DbConfig `json:"mysql"`
	BoltDb DbConfig `json:"bolt"`
	Postgres DbConfig `json:"pgsql"`

	// Format `:port_num` eg, :3000
	// if : is missing it will be corrected
	Port string `json:"port"`

	// Interface ip, put in front of the port.
	// defaults to empty
	Interface string `json:"interface"`

	// semaphore stores ephemeral projects here
	TmpPath string `json:"tmp_path"`

	// cookie hashing & encryption
	CookieHash       string `json:"cookie_hash"`
	CookieEncryption string `json:"cookie_encryption"`

	// email alerting
	EmailSender string `json:"email_sender"`
	EmailHost   string `json:"email_host"`
	EmailPort   string `json:"email_port"`

	// web host
	WebHost string `json:"web_host"`

	// ldap settings
	LdapBindDN       string       `json:"ldap_binddn"`
	LdapBindPassword string       `json:"ldap_bindpassword"`
	LdapServer       string       `json:"ldap_server"`
	LdapSearchDN     string       `json:"ldap_searchdn"`
	LdapSearchFilter string       `json:"ldap_searchfilter"`
	LdapMappings     ldapMappings `json:"ldap_mappings"`

	// telegram alerting
	TelegramChat  string `json:"telegram_chat"`
	TelegramToken string `json:"telegram_token"`

	// task concurrency
	ConcurrencyMode  string `json:"concurrency_mode"`
	MaxParallelTasks int    `json:"max_parallel_tasks"`

	// configType field ordering with bools at end reduces struct size
	// (maligned check)

	// feature switches
	EmailAlert    bool `json:"email_alert"`
	TelegramAlert bool `json:"telegram_alert"`
	LdapEnable    bool `json:"ldap_enable"`
	LdapNeedTLS   bool `json:"ldap_needtls"`
}

//Config exposes the application configuration storage for use in the application
var Config *ConfigType

// ToJSON returns a JSON string of the config
func (conf *ConfigType) ToJSON() ([]byte, error) {
	return json.MarshalIndent(&conf, " ", "\t")
}

// ConfigInit reads in cli flags, and switches actions appropriately on them
func ConfigInit(configPath string) {
	loadConfig(configPath)
	validateConfig()

	var encryption []byte

	hash, _ := base64.StdEncoding.DecodeString(Config.CookieHash)
	if len(Config.CookieEncryption) > 0 {
		encryption, _ = base64.StdEncoding.DecodeString(Config.CookieEncryption)
	}

	Cookie = securecookie.New(hash, encryption)
	WebHostURL, _ = url.Parse(Config.WebHost)
	if len(WebHostURL.String()) == 0 {
		WebHostURL = nil
	}
}

func loadConfig(configPath string) {
	//If the configPath option has been set try to load and decode it
	var usedPath string
	if len(configPath) > 0 {
		path := configPath
		file, err := os.Open(path)
		exitOnConfigError(err)
		decodeConfig(file)
		usedPath = path
	} else {
		// if no configPath look in the cwd
		cwd, err := os.Getwd()
		exitOnConfigError(err)
		defaultPath := path.Join(cwd, "config.json")
		file, err := os.Open(defaultPath)
		exitOnConfigError(err)
		decodeConfig(file)
		usedPath = defaultPath
	}

	fmt.Println("Using config file: " + usedPath)
}

func validateConfig() {

	validatePort()

	if len(Config.TmpPath) == 0 {
		Config.TmpPath = "/tmp/semaphore"
	}

	if Config.MaxParallelTasks < 1 {
		Config.MaxParallelTasks = 10
	}
}

func validatePort() {

	//TODO - why do we do this only with this variable?
	if len(os.Getenv("PORT")) > 0 {
		Config.Port = ":" + os.Getenv("PORT")
	}
	if len(Config.Port) == 0 {
		Config.Port = ":3000"
	}
	if !strings.HasPrefix(Config.Port, ":") {
		Config.Port = ":" + Config.Port
	}
}

func exitOnConfigError(err error) {
	if err != nil {
		fmt.Println("Cannot Find configuration! Use -config parameter to point to a JSON file generated by -setup.\n\n Hint: have you run `-setup` ?")
		os.Exit(1)
	}
}

func decodeConfig(file io.Reader) {
	if err := json.NewDecoder(file).Decode(&Config); err != nil {
		fmt.Println("Could not decode configuration!")
		panic(err)
	}
}

// String returns dialect name for GORP.
// TODO: It should be moved to sql package.
func (d DbDriver) String() string {
	return [...]string{
		"mysql",
		"", // didn't support by BoltDB
		"postgres",
	}[d]
}

func (d *DbConfig) IsPresent() bool {
	return d.Hostname != ""
}

func (d *DbConfig) HasSupportMultipleDatabases() bool {
	return true
}

func (d *DbConfig) GetConnectionString(includeDbName bool) (connectionString string, err error) {
	switch d.Dialect {
	case DbDriverBolt:
		connectionString = d.Hostname
	case DbDriverMySQL:
		if includeDbName {
			connectionString = fmt.Sprintf(
				"%s:%s@tcp(%s)/%s?parseTime=true&interpolateParams=true",
				d.Username,
				d.Password,
				d.Hostname,
				d.DbName)
		} else {
			connectionString = fmt.Sprintf(
				"%s:%s@tcp(%s)/?parseTime=true&interpolateParams=true",
				d.Username,
				d.Password,
				d.Hostname)
		}
	case DbDriverPostgres:
		if includeDbName {
			connectionString = fmt.Sprintf(
				"postgres://%s:%s@%s/%s",
				d.Username,
				d.Password,
				d.Hostname,
				d.DbName)
		} else {
			connectionString = fmt.Sprintf(
				"postgres://%s:%s@%s/",
				d.Username,
				d.Password,
				d.Hostname)
		}
	default:
		err = fmt.Errorf("unsupported database driver: %s", d.Dialect)
	}
	return
}

func (conf *ConfigType) GetDBConfig() (dbConfig DbConfig, err error) {
	switch {
	case conf.MySQL.IsPresent():
		dbConfig = conf.MySQL
		dbConfig.Dialect = DbDriverMySQL
	case conf.BoltDb.IsPresent():
		dbConfig = conf.BoltDb
		dbConfig.Dialect = DbDriverBolt
	case conf.Postgres.IsPresent():
		dbConfig = conf.Postgres
		dbConfig.Dialect = DbDriverPostgres
	default:
		err = errors.New("database configuration not found")
	}
	return
}

//GenerateCookieSecrets generates cookie secret during setup
func (conf *ConfigType) GenerateCookieSecrets() {
	hash := securecookie.GenerateRandomKey(32)
	encryption := securecookie.GenerateRandomKey(32)

	conf.CookieHash = base64.StdEncoding.EncodeToString(hash)
	conf.CookieEncryption = base64.StdEncoding.EncodeToString(encryption)
}
