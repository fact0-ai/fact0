package audit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// EvidencePack builds a ZIP with PDF, verification JSON, README, and evidence limitations.
func EvidencePack(ctx context.Context, svc *Service, w io.Writer, in PDFInput) error {
	from, to := in.From, in.To
	if from.IsZero() {
		from = historyStart
	}
	if to.IsZero() {
		to = farFuture
	}
	in.From, in.To = from, to

	verify, err := svc.Verify(ctx, in.TenantID, from, to)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	pdfBuf := &bytes.Buffer{}
	if err := RenderPDF(ctx, pdfBuf, svc, in); err != nil {
		return err
	}
	if err := writeZipFile(zw, "audit-report.pdf", pdfBuf.Bytes()); err != nil {
		return err
	}

	verifyJSON, _ := json.MarshalIndent(verify, "", "  ")
	if err := writeZipFile(zw, "verification.json", verifyJSON); err != nil {
		return err
	}

	readme := fmt.Sprintf(`Fact0 Evidence Pack
Tenant: %s (%s)
Generated: %s
Period: %s to %s

Contents:
- audit-report.pdf - recorded audit report
- verification.json - chain integrity result (valid=%v, events_checked=%d)
- control-mapping.txt - evidence limitations (legacy filename)`,
		in.TenantName, in.TenantID, time.Now().UTC().Format(time.RFC3339),
		from.Format(time.RFC3339), to.Format(time.RFC3339),
		verify.Valid, verify.EventsChecked)

	if len(svc.signingKey) > 0 {
		pdfSig := ed25519.Sign(svc.signingKey, pdfBuf.Bytes())
		verifySig := ed25519.Sign(svc.signingKey, verifyJSON)
		pubKey := svc.signingKey.Public().(ed25519.PublicKey)

		manifest := map[string]string{
			"public_key":        base64.StdEncoding.EncodeToString(pubKey),
			"audit-report.pdf":  base64.StdEncoding.EncodeToString(pdfSig),
			"verification.json": base64.StdEncoding.EncodeToString(verifySig),
		}
		manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
		if err := writeZipFile(zw, "manifest.sig", manifestJSON); err != nil {
			return err
		}
		readme += "\n- manifest.sig - Ed25519 cryptographic signatures for the exported files"
	}

	readme += `

Use authenticated GET /v1/verify to check the stored chain. Verify file
signatures with scripts/verify-export.py from the Fact0 repository and a public
key obtained separately from the instance through a trusted channel.
The embedded public key alone does not establish trust.

This experimental release has best-effort maintenance and no SLA.
`
	if err := writeZipFile(zw, "README.txt", []byte(readme)); err != nil {
		return err
	}

	controls := `Evidence limitations
This archive is not SOC 2, HIPAA or other compliance certification.
A valid hash chain checks consistency of delivered records, not complete capture.
An export signature binds signed files to an instance key; trust that key separately.
Administrators who control storage and signing keys can replace a complete history.
Replay reconstructs recorded events and does not rerun external tools.
`
	if err := writeZipFile(zw, "control-mapping.txt", []byte(controls)); err != nil {
		return err
	}

	return zw.Close()
}

func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}
