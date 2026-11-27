package upload_test

import (
	"testing"

	"github.com/standards-lab/go-web-sdk/upload"
	"github.com/standards-lab/go-web-sdk/upload/uploadtest"
)

func TestMemoryStore(t *testing.T) {
	uploadtest.Run(t, func() upload.Store { return upload.NewMemoryStore() })
}
