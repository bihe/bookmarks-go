package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const configString = `{
    "security": {
        "jwtIssuer": "login.binggl.net",
        "jwtSecret": "secret",
	"cookieName": "login_token",
	"loginRedirect": "https://login.url.com",
        "claim": {
            "name": "bookmarks",
            "url": "http://localhost:3000",
            "roles": ["User", "Admin"]
	},
	"cacheDuration": "10m"
    },
    "database": {
	"connectionString": "./bookmarks.db"
    },
    "logging": {
	"filePath": "/temp/file",
	"logLevel": "debug"
    },
    "cookies": {
	"domain": "example.com",
	"path": "/",
	"secure": true,
	"prefix": "prefix"
    },
    "startUrl": "http://url",
    "environment": "Development"
}`

// TestConfigReader reads config settings from json
func TestConfigReader(t *testing.T) {
	reader := strings.NewReader(configString)
	config, err := GetSettings(reader)
	if err != nil {
		t.Error("Could not read.", err)
	}
	assert.Equal(t, "./bookmarks.db", config.DB.ConnStr)

	assert.Equal(t, "https://login.url.com", config.Sec.LoginRedirect)
	assert.Equal(t, "bookmarks", config.Sec.Claim.Name)
	assert.Equal(t, "secret", config.Sec.JwtSecret)
	assert.Equal(t, "10m", config.Sec.CacheDuration)

	assert.Equal(t, "/temp/file", config.Log.FilePath)
	assert.Equal(t, "debug", config.Log.LogLevel)

	assert.Equal(t, "example.com", config.Cookies.Domain)
	assert.Equal(t, "/", config.Cookies.Path)
	assert.Equal(t, "prefix", config.Cookies.Prefix)
	assert.Equal(t, true, config.Cookies.Secure)

	assert.Equal(t, "http://url", config.StartURL)
	assert.Equal(t, "Development", config.Environment)
}
