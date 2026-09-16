package audit

import (
	"context"
	"io"
	"time"
)

// PDFInput bundles everything the renderer needs from the caller.
type PDFInput struct {
	TenantID   string
	TenantName string
	From       time.Time
	To         time.Time
	VerifyURL  string // human-readable URL printed in the PDF footer
}

// RenderPDF writes a recorded audit-trail PDF for `in` to w.
func RenderPDF(ctx context.Context, w io.Writer, svc *Service, in PDFInput) error {
	return renderPDF(ctx, w, svc, in)
}
