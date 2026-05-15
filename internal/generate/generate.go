package generate

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"xct/internal/domain"
)

type Files struct {
	IranConfig   []byte
	OuterConfig  []byte
	NginxSite    []byte
	IranService  []byte
	OuterService []byte
}

func All(p domain.Profile) (Files, error) {
	if err := p.Validate(); err != nil {
		return Files{}, err
	}
	iran, err := IranXrayConfig(p)
	if err != nil {
		return Files{}, err
	}
	outer, err := OuterXrayConfig(p)
	if err != nil {
		return Files{}, err
	}
	return Files{
		IranConfig:   iran,
		OuterConfig:  outer,
		NginxSite:    []byte(NginxSite(p)),
		IranService:  []byte(IranService(p)),
		OuterService: []byte(OuterService(p)),
	}, nil
}

func NginxSite(p domain.Profile) string {
	return fmt.Sprintf(`server {
    listen %[1]d ssl;
    listen [::]:%[1]d ssl;

    server_name %[2]s;

    ssl_certificate     %[3]s;
    ssl_certificate_key %[4]s;

    ssl_protocols TLSv1.2 TLSv1.3;

    access_log off;

    location %[5]s {
        proxy_pass http://127.0.0.1:%[6]d;

        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;

        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_buffering off;
        proxy_request_buffering off;
        proxy_max_temp_file_size 0;
        proxy_socket_keepalive on;

        proxy_read_timeout 86400;
        proxy_send_timeout 86400;
        send_timeout 86400;
    }

    location / {
        return 404;
    }
}
`, p.CDNPort, p.Domain, p.SSLCrt, p.SSLKey, p.WSPath, p.BackendPort)
}

func IranService(p domain.Profile) string {
	return fmt.Sprintf(`[Unit]
Description=XCT %s profile %s on Iran
After=network.target nginx.service
Wants=nginx.service

[Service]
ExecStart=%s run -config %s
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`, p.Type, p.Profile, p.XrayBin, p.IranXrayConfig)
}

func OuterService(p domain.Profile) string {
	return fmt.Sprintf(`[Unit]
Description=XCT %s profile %s on outer VPS
After=network.target

[Service]
ExecStart=%s run -config %s
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`, p.Type, p.Profile, p.RemoteXrayBin, p.OuterXrayConfig)
}

func IranXrayConfig(p domain.Profile) ([]byte, error) {
	if p.Type == domain.Reverse {
		return marshal(iranReverse(p))
	}
	return marshal(iranDirect(p))
}

func OuterXrayConfig(p domain.Profile) ([]byte, error) {
	if p.Type == domain.Reverse {
		return marshal(outerReverse(p))
	}
	return marshal(outerDirect(p))
}

func OutboundSnippet(p domain.Profile) string {
	if p.Type == domain.Reverse {
		return fmt.Sprintf(`Use this in Iran x-ui as an outbound to exit from outer VPS:
{
  "tag": "reverse-%[1]s-vless-to-outer",
  "protocol": "vless",
  "settings": {
    "vnext": [
      {
        "address": "127.0.0.1",
        "port": %[2]d,
        "users": [
          {
            "id": "%[3]s",
            "encryption": "none"
          }
        ]
      }
    ]
  },
  "streamSettings": {
    "network": "tcp",
    "security": "none"
  },
  "mux": {
    "enabled": false
  }
}

SOCKS alternative on Iran:
socks5://%[4]s:%[5]s@127.0.0.1:%[6]d
`, p.Profile, p.IranLocalVLESSPort, p.IranLocalVLESSUUID, p.IranSocksUser, p.IranSocksPass, p.IranSocksPort)
	}
	return fmt.Sprintf(`Use this in outer x-ui as an outbound to exit from Iran:
{
  "tag": "direct-%[1]s-vless-to-iran",
  "protocol": "vless",
  "settings": {
    "vnext": [
      {
        "address": "127.0.0.1",
        "port": %[2]d,
        "users": [
          {
            "id": "%[3]s",
            "encryption": "none"
          }
        ]
      }
    ]
  },
  "streamSettings": {
    "network": "tcp",
    "security": "none"
  },
  "mux": {
    "enabled": false
  }
}

SOCKS alternative on outer:
socks5://%[4]s:%[5]s@127.0.0.1:%[6]d
`, p.Profile, p.OuterLocalVLESSPort, p.OuterLocalVLESSUUID, p.OuterSocksUser, p.OuterSocksPass, p.OuterSocksPort)
}

func NginxEnabledPath(site string) string {
	return filepath.Join("/etc/nginx/sites-enabled", filepath.Base(site))
}

func marshal(value any) ([]byte, error) {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func iranReverse(p domain.Profile) map[string]any {
	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{
				"tag":      "iran-socks-in-" + p.Profile,
				"listen":   p.IranSocksListen,
				"port":     p.IranSocksPort,
				"protocol": "socks",
				"settings": map[string]any{
					"auth":     "password",
					"accounts": []any{map[string]any{"user": p.IranSocksUser, "pass": p.IranSocksPass}},
					"udp":      true,
				},
				"sniffing": sniffing(),
			},
			localVLESSInbound("iran-local-vless-in-"+p.Profile, p.IranLocalVLESSListen, p.IranLocalVLESSPort, p.IranLocalVLESSUUID, p.Profile),
			map[string]any{
				"tag":      "xray-reverse-vless-in-" + p.Profile,
				"listen":   "127.0.0.1",
				"port":     p.BackendPort,
				"protocol": "vless",
				"settings": map[string]any{
					"decryption": "none",
					"clients": []any{map[string]any{
						"id":    p.RemoteUUID,
						"email": "outer-bridge@" + p.Profile,
						"reverse": map[string]any{
							"tag": "reverse-to-outer-" + p.Profile,
						},
					}},
				},
				"streamSettings": wsNone(p.WSPath),
			},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				blockUDP443([]string{"iran-socks-in-" + p.Profile, "iran-local-vless-in-" + p.Profile}),
				map[string]any{
					"type":        "field",
					"inboundTag":  []string{"iran-socks-in-" + p.Profile, "iran-local-vless-in-" + p.Profile},
					"outboundTag": "reverse-to-outer-" + p.Profile,
				},
			},
		},
		"outbounds": []any{
			map[string]any{"tag": "direct-placeholder", "protocol": "freedom"},
			map[string]any{"tag": "blocked", "protocol": "blackhole"},
		},
	}
}

func outerReverse(p domain.Profile) map[string]any {
	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				map[string]any{
					"type":        "field",
					"inboundTag":  []string{"reverse-from-iran-" + p.Profile},
					"outboundTag": "outer-direct",
				},
			},
		},
		"outbounds": []any{
			map[string]any{
				"tag":      "outer-direct",
				"protocol": "freedom",
				"settings": map[string]any{"domainStrategy": "UseIPv4"},
			},
			map[string]any{
				"tag":      "connect-to-iran-portal-" + p.Profile,
				"protocol": "vless",
				"settings": map[string]any{
					"address":    p.Domain,
					"port":       p.CDNPort,
					"id":         p.RemoteUUID,
					"encryption": "none",
					"reverse": map[string]any{
						"tag":      "reverse-from-iran-" + p.Profile,
						"sniffing": sniffing(),
					},
				},
				"streamSettings": wsTLS(p),
			},
		},
	}
}

func iranDirect(p domain.Profile) map[string]any {
	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{
				"tag":      "direct-vless-ws-in-" + p.Profile,
				"listen":   "127.0.0.1",
				"port":     p.BackendPort,
				"protocol": "vless",
				"settings": map[string]any{
					"decryption": "none",
					"clients": []any{map[string]any{
						"id":    p.RemoteUUID,
						"email": "outer-direct@" + p.Profile,
					}},
				},
				"streamSettings": wsNone(p.WSPath),
				"sniffing":       sniffing(),
			},
		},
		"outbounds": []any{
			map[string]any{"tag": "iran-direct", "protocol": "freedom", "settings": map[string]any{"domainStrategy": "UseIPv4"}},
			map[string]any{"tag": "blocked", "protocol": "blackhole"},
		},
		"routing": map[string]any{
			"domainStrategy": "UseIPv4",
			"rules": []any{
				map[string]any{"type": "field", "network": "udp", "port": "443", "outboundTag": "blocked"},
				map[string]any{"type": "field", "inboundTag": []string{"direct-vless-ws-in-" + p.Profile}, "outboundTag": "iran-direct"},
			},
		},
	}
}

func outerDirect(p domain.Profile) map[string]any {
	inbounds := []any{
		localVLESSInbound("outer-local-vless-in-"+p.Profile, p.OuterLocalVLESSListen, p.OuterLocalVLESSPort, p.OuterLocalVLESSUUID, p.Profile),
		map[string]any{
			"tag":      "outer-local-socks-in-" + p.Profile,
			"listen":   p.OuterSocksListen,
			"port":     p.OuterSocksPort,
			"protocol": "socks",
			"settings": map[string]any{
				"auth":     "password",
				"accounts": []any{map[string]any{"user": p.OuterSocksUser, "pass": p.OuterSocksPass}},
				"udp":      false,
			},
			"sniffing": sniffing(),
		},
	}
	return map[string]any{
		"log":      map[string]any{"loglevel": "warning"},
		"inbounds": inbounds,
		"outbounds": []any{
			map[string]any{
				"tag":      "to-iran-vless-wss-" + p.Profile,
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{map[string]any{
						"address": p.Domain,
						"port":    p.CDNPort,
						"users":   []any{map[string]any{"id": p.RemoteUUID, "encryption": "none"}},
					}},
				},
				"streamSettings": wsTLS(p),
				"mux":            map[string]any{"enabled": false},
			},
			map[string]any{"tag": "blocked", "protocol": "blackhole"},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				blockUDP443([]string{"outer-local-vless-in-" + p.Profile, "outer-local-socks-in-" + p.Profile}),
				map[string]any{
					"type":        "field",
					"inboundTag":  []string{"outer-local-vless-in-" + p.Profile, "outer-local-socks-in-" + p.Profile},
					"outboundTag": "to-iran-vless-wss-" + p.Profile,
				},
			},
		},
	}
}

func localVLESSInbound(tag, listen string, port int, uuid, profile string) map[string]any {
	return map[string]any{
		"tag":      tag,
		"listen":   listen,
		"port":     port,
		"protocol": "vless",
		"settings": map[string]any{
			"decryption": "none",
			"clients": []any{map[string]any{
				"id":    uuid,
				"email": "local-xui@" + profile,
			}},
		},
		"streamSettings": map[string]any{"network": "tcp", "security": "none"},
		"sniffing":       sniffing(),
	}
}

func sniffing() map[string]any {
	return map[string]any{"enabled": true, "destOverride": []string{"http", "tls"}}
}

func wsNone(path string) map[string]any {
	return map[string]any{
		"network":  "ws",
		"security": "none",
		"wsSettings": map[string]any{
			"path": path,
		},
	}
}

func wsTLS(p domain.Profile) map[string]any {
	return map[string]any{
		"network":  "ws",
		"security": "tls",
		"tlsSettings": map[string]any{
			"serverName":    p.Domain,
			"allowInsecure": false,
		},
		"wsSettings": map[string]any{
			"path":            p.WSPath,
			"headers":         map[string]any{"Host": p.Domain},
			"heartbeatPeriod": 10,
		},
	}
}

func blockUDP443(tags []string) map[string]any {
	return map[string]any{
		"type":        "field",
		"inboundTag":  tags,
		"network":     "udp",
		"port":        "443",
		"outboundTag": "blocked",
	}
}
