package audit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeRepoWithCounts extends the test fakeRepo with simple in-memory
// implementations of CountByAction/CountByActorType so the PDF renderer
// has tabular data to draw.
type fakeRepoWithCounts struct{ *fakeRepo }

func (r *fakeRepoWithCounts) CountByAction(ctx context.Context, t string, from, to time.Time) (map[string]int, error) {
	out := map[string]int{}
	for _, e := range r.rows[t] {
		out[e.Action]++
	}
	return out, nil
}
func (r *fakeRepoWithCounts) CountByActorType(ctx context.Context, t string, from, to time.Time) (map[string]int, error) {
	out := map[string]int{}
	for _, e := range r.rows[t] {
		out[string(e.Actor.Type)]++
	}
	return out, nil
}

func TestRenderPDF_ProducesValidNonEmptyPDF(t *testing.T) {
	t.Parallel()
	repo := &fakeRepoWithCounts{newFakeRepo()}
	svc := NewService(repo, zerologDiscard(), false)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := svc.Log(ctx, "tenant_pdf", sampleEvent("doc.read")); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var buf bytes.Buffer
	err := RenderPDF(ctx, &buf, svc, PDFInput{
		TenantID:   "tenant_pdf",
		TenantName: "PDF Co",
		From:       time.Time{},
		To:         time.Time{},
		VerifyURL:  "http://example.com/v1/verify",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if buf.Len() < 1024 {
		t.Fatalf("pdf too small: %d bytes", buf.Len())
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF: starts with %q", buf.Bytes()[:8])
	}
	if !strings.HasSuffix(strings.TrimRight(buf.String(), "\n\r "), "%%EOF") {
		t.Fatalf("pdf does not end with %%EOF marker")
	}
}

func TestEvidencePack_WithSigningKey(t *testing.T) {
	t.Parallel()
	repo := &fakeRepoWithCounts{newFakeRepo()}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	svc := NewServiceWithOptions(repo, zerologDiscard(), false, ServiceOptions{
		SigningKey: priv,
	})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := svc.Log(ctx, "tenant_pdf", sampleEvent("doc.read")); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var buf bytes.Buffer
	err = EvidencePack(ctx, svc, &buf, PDFInput{
		TenantID:   "tenant_pdf",
		TenantName: "PDF Co",
		From:       time.Time{},
		To:         time.Time{},
		VerifyURL:  "http://example.com/v1/verify",
	})
	if err != nil {
		t.Fatalf("evidence pack: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}

	files := map[string][]byte{}
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = body
	}
	var signatures map[string]string
	if err := json.Unmarshal(files["manifest.sig"], &signatures); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"audit-report.pdf", "verification.json"} {
		sig, err := base64.StdEncoding.DecodeString(signatures[name])
		if err != nil {
			t.Fatal(err)
		}
		if !ed25519.Verify(pub, files[name], sig) {
			t.Fatalf("invalid signature for %s", name)
		}
		tampered := append([]byte(nil), files[name]...)
		tampered[0] ^= 1
		if ed25519.Verify(pub, tampered, sig) {
			t.Fatalf("tampered %s verified", name)
		}
	}
	foundManifest := false
	for _, f := range zr.File {
		if f.Name == "manifest.sig" {
			foundManifest = true
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open manifest: %v", err)
			}
			var manifest map[string]string
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
				t.Fatalf("decode manifest: %v", err)
			}
			_ = rc.Close()
			pkBase64 := manifest["public_key"]
			expectedPK := base64.StdEncoding.EncodeToString(pub)
			if pkBase64 != expectedPK {
				t.Errorf("expected public key %q, got %q", expectedPK, pkBase64)
			}
		}
	}
	if !foundManifest {
		t.Errorf("manifest.sig not found in evidence pack")
	}
}
