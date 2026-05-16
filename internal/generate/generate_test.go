package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"xct/internal/domain"
)

func TestReverseConfigsAreJSONAndContainReverseTags(t *testing.T) {
	p := sampleReverse()
	iran, err := IranXrayConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := OuterXrayConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(iran) || !json.Valid(outer) {
		t.Fatalf("generated configs must be valid JSON")
	}
	if !strings.Contains(string(iran), "reverse-to-outer-demo") {
		t.Fatalf("Iran reverse config missing reverse outbound tag:\n%s", iran)
	}
	if !strings.Contains(string(outer), "connect-to-iran-portal-demo") {
		t.Fatalf("outer reverse config missing portal outbound:\n%s", outer)
	}
	for _, want := range []string{`"network": "xhttp"`, `"mode": "packet-up"`, `"mode": "auto"`, `"alpn": [`} {
		if !strings.Contains(string(iran)+string(outer), want) {
			t.Fatalf("reverse configs missing XHTTP setting %q\nIran:\n%s\nOuter:\n%s", want, iran, outer)
		}
	}
	if strings.Contains(string(iran)+string(outer), `"network": "ws"`) {
		t.Fatalf("reverse configs should not use WebSocket transport\nIran:\n%s\nOuter:\n%s", iran, outer)
	}
}

func TestDirectOutboundSnippetUsesOuterLocalVLESS(t *testing.T) {
	p := sampleDirect()
	snippet := OutboundSnippet(p)
	if !strings.Contains(snippet, `"port": 20151`) {
		t.Fatalf("snippet should point at outer local VLESS port:\n%s", snippet)
	}
	if !strings.Contains(snippet, "direct-demo-vless-to-iran") {
		t.Fatalf("snippet should contain direct tag:\n%s", snippet)
	}
}

func TestReverseNginxSiteProxiesXHTTPOverHTTP2(t *testing.T) {
	site := NginxSite(sampleReverse())
	for _, want := range []string{
		"listen 2087 ssl http2;",
		"location /xct-reverse-demo",
		"proxy_http_version 1.1;",
		"proxy_set_header Host $http_host;",
		"proxy_pass http://127.0.0.1:18191;",
	} {
		if !strings.Contains(site, want) {
			t.Fatalf("nginx site missing %q:\n%s", want, site)
		}
	}
	for _, unwanted := range []string{"proxy_set_header Upgrade", `proxy_set_header Connection "upgrade"`} {
		if strings.Contains(site, unwanted) {
			t.Fatalf("reverse nginx site should not contain WebSocket header %q:\n%s", unwanted, site)
		}
	}
}

func TestDirectNginxSiteKeepsWebSocketProxy(t *testing.T) {
	site := NginxSite(sampleDirect())
	for _, want := range []string{
		"listen 2083 ssl;",
		"location /xct-direct-demo",
		"proxy_set_header Upgrade $http_upgrade;",
		`proxy_set_header Connection "upgrade";`,
		"proxy_pass http://127.0.0.1:18192;",
	} {
		if !strings.Contains(site, want) {
			t.Fatalf("direct nginx site missing %q:\n%s", want, site)
		}
	}
}

func sampleReverse() domain.Profile {
	p := domain.Profile{
		Profile:              "demo",
		Type:                 domain.Reverse,
		Domain:               "sky.example.com",
		CDNPort:              2087,
		WSPath:               "/xct-reverse-demo",
		SSLCrt:               "/etc/ssl/cert.pem",
		SSLKey:               "/etc/ssl/key.pem",
		XrayBin:              domain.DefaultXrayBin,
		RemoteXrayBin:        domain.DefaultXrayBin,
		BackendPort:          18191,
		RemoteUUID:           "remote-uuid",
		LocalUUID:            "local-uuid",
		IranSocksListen:      "127.0.0.1",
		IranSocksPort:        20141,
		IranSocksUser:        "rain",
		IranSocksPass:        "2013",
		IranLocalVLESSListen: "127.0.0.1",
		IranLocalVLESSPort:   20142,
		IranLocalVLESSUUID:   "local-uuid",
		SSHHost:              "outer.example.com",
		SSHPort:              22,
		SSHUser:              "root",
		SSHAuth:              domain.SSHKey,
		RemoteRootMode:       "root",
	}
	p.FillDerivedPaths("/etc/xray-cdn-tunnel-controller")
	return p
}

func sampleDirect() domain.Profile {
	p := sampleReverse()
	p.Type = domain.Direct
	p.CDNPort = 2083
	p.WSPath = "/xct-direct-demo"
	p.BackendPort = 18192
	p.IranSocksPort = 0
	p.IranLocalVLESSPort = 0
	p.IranLocalVLESSUUID = ""
	p.OuterLocalVLESSListen = "127.0.0.1"
	p.OuterLocalVLESSPort = 20151
	p.OuterLocalVLESSUUID = "outer-local-uuid"
	p.OuterSocksListen = "127.0.0.1"
	p.OuterSocksPort = 20152
	p.OuterSocksUser = "rain"
	p.OuterSocksPass = "2013"
	p.FillDerivedPaths("/etc/xray-cdn-tunnel-controller")
	return p
}
