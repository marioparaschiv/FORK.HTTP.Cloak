package transport

import (
	"encoding/base64"
	"testing"

	"github.com/sardanioss/httpcloak/dns"
	"github.com/sardanioss/httpcloak/fingerprint"
	utls "github.com/sardanioss/utls"
)

func rawQUICHello(t *testing.T) []byte {
	t.Helper()
	spec, err := utls.UTLSIdToSpecWithSeed(utls.HelloChrome_146_QUIC, 1)
	if err != nil {
		t.Fatalf("build QUIC spec: %v", err)
	}
	conn := utls.UClient(nil, &utls.Config{ServerName: "example.com", OmitEmptyPsk: true}, utls.HelloCustom)
	if err := conn.ApplyPreset(&spec); err != nil {
		t.Fatalf("apply QUIC spec: %v", err)
	}
	if err := conn.BuildHandshakeState(); err != nil {
		t.Fatalf("build handshake state: %v", err)
	}
	hello := conn.HandshakeState.Hello.Raw
	return append([]byte{0x16, 0x03, 0x01, byte(len(hello) >> 8), byte(len(hello))}, hello...)
}

// The session cache is what lets HTTP/3 resume at all, and it is attached only
// when the preset has a resumption hello to send. A captured QUIC PSK hello
// has to count, or a raw QUIC preset never resumes however it was written.
func TestRawQUICPSKCaptureEnablesTheSessionCache(t *testing.T) {
	quic := base64.StdEncoding.EncodeToString(rawQUICHello(t))

	for _, tc := range []struct {
		name, tls string
		want      bool
	}{
		{"with PSK capture", `"raw_quic_client_hello":"` + quic + `","raw_quic_psk_client_hello":"` + quic + `"`, true},
		{"without PSK capture", `"raw_quic_client_hello":"` + quic + `"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := fingerprint.LoadAndBuildPresetFromJSON([]byte(`{"preset":{"name":"h3-raw-quic",` +
				`"based_on":"chrome-latest","tls":{` + tc.tls + `}}}`))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			tr, err := NewHTTP3Transport(p, dns.NewCache())
			if err != nil {
				t.Fatalf("NewHTTP3Transport: %v", err)
			}
			defer tr.Close()

			if got := tr.tlsConfig.ClientSessionCache != nil; got != tc.want {
				t.Errorf("session cache attached = %v, want %v", got, tc.want)
			}
			if spec := tr.getSpecForHost("example.com"); !fingerprint.SpecHasQUICTransportParameters(spec) {
				t.Error("the dial spec is not the QUIC capture")
			}
		})
	}
}
