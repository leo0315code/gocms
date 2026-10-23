package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 应用配置，全部来自环境变量 / .env 文件
type Config struct {
	Env      string
	Name     string
	Port     string
	URL      string
	IsDev    bool
	Location *time.Location

	DBHost   string
	DBPort   string
	DBUser   string
	DBPass   string
	DBName   string
	DBParams string

	SessionName     string
	SessionLifetime time.Duration
	SessionSecure   bool

	UploadDir   string
	UploadMaxMB int64

	Theme    string
	PageSize int

	AdminUser  string
	AdminPass  string
	AdminEmail string

	// AdminPath 后台入口路径（不含斜杠），仅作为初始值；
	// 运行期以数据库 settings.admin_path 为准，可在后台可视化修改。
	AdminPath string

	// TrustProxy 是否信任反向代理传来的 X-Real-IP / X-Forwarded-For。
	//
	// 默认 false：这两个头任何客户端都能伪造，信任它们等于让所有基于 IP 的
	// 限流与白名单形同虚设（攻击者每次换个头就绕过）。
	// 只有确定前面挂了 Nginx 等代理、且代理会覆盖该头时，才设为 true。
	TrustProxy bool
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getint(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// LoadDotEnv 读取 .env 文件，已存在的环境变量优先
func LoadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}

// Load 加载并校验配置
func Load() (*Config, error) {
	loc, err := time.LoadLocation(getenv("TZ", "Asia/Shanghai"))
	if err != nil {
		loc = time.Local
	}

	env := getenv("APP_ENV", "development")
	c := &Config{
		Env:             env,
		Name:            getenv("APP_NAME", "GoCMS"),
		Port:            getenv("APP_PORT", "8090"),
		URL:             strings.TrimRight(getenv("APP_URL", "http://localhost:8090"), "/"),
		IsDev:           env != "production",
		Location:        loc,
		DBHost:          getenv("DB_HOST", "127.0.0.1"),
		DBPort:          getenv("DB_PORT", "3306"),
		DBUser:          getenv("DB_USER", "root"),
		DBPass:          getenv("DB_PASS", ""),
		DBName:          getenv("DB_NAME", "gocms"),
		DBParams:        getenv("DB_PARAMS", "charset=utf8mb4&parseTime=true&loc=Local&collation=utf8mb4_unicode_ci"),
		SessionName:     getenv("SESSION_NAME", "gocms_session"),
		SessionLifetime: time.Duration(getint("SESSION_LIFETIME", 86400)) * time.Second,
		SessionSecure:   env == "production",
		UploadDir:       getenv("UPLOAD_DIR", "./uploads"),
		UploadMaxMB:     int64(getint("UPLOAD_MAX_MB", 10)),
		Theme:           getenv("THEME", "default"),
		PageSize:        getint("PAGE_SIZE", 10),
		AdminUser:       getenv("ADMIN_USER", "admin"),
		AdminPass:       getenv("ADMIN_PASS", "Admin@123456"),
		AdminEmail:      getenv("ADMIN_EMAIL", "admin@example.com"),
		AdminPath:       getenv("ADMIN_PATH", "admin"),
		TrustProxy:      getenv("TRUST_PROXY", "") == "1",
	}
	if c.DBName == "" {
		return nil, fmt.Errorf("配置错误：DB_NAME 不能为空")
	}
	if c.PageSize <= 0 {
		c.PageSize = 10
	}
	return c, nil
}

// DSN 构造 MySQL 连接串
func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", c.DBUser, c.DBPass, c.DBHost, c.DBPort, c.DBName, c.DBParams)
}

// DSNWithoutDB 用于建库前的连接
func (c *Config) DSNWithoutDB() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/?%s", c.DBUser, c.DBPass, c.DBHost, c.DBPort, c.DBParams)
}
