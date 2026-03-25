package volcengine

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

func TestConvertImageRequestEditsMultipartToDataURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "doubao-seedream-4-0-250828"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.WriteField("prompt", "edit this image"); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}

	imagePart, err := writer.CreateFormFile("image", "test.png")
	if err != nil {
		t.Fatalf("create image part: %v", err)
	}
	if _, err = imagePart.Write([]byte("fake image bytes")); err != nil {
		t.Fatalf("write image part: %v", err)
	}

	maskPart, err := writer.CreateFormFile("mask", "mask.png")
	if err != nil {
		t.Fatalf("create mask part: %v", err)
	}
	if _, err = maskPart.Write([]byte("fake mask bytes")); err != nil {
		t.Fatalf("write mask part: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/images/edits", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertImageRequest(ctx, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}, dto.ImageRequest{
		Model:  "doubao-seedream-4-0-250828",
		Prompt: "edit this image",
	})
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	payload, ok := converted.(map[string]any)
	if !ok {
		t.Fatalf("unexpected converted type %T", converted)
	}

	if got, ok := payload["image"].(string); !ok || !strings.HasPrefix(got, "data:text/plain; charset=utf-8;base64,") {
		t.Fatalf("expected image data url, got %#v", payload["image"])
	}

	if got, ok := payload["mask"].(string); !ok || !strings.HasPrefix(got, "data:text/plain; charset=utf-8;base64,") {
		t.Fatalf("expected mask data url, got %#v", payload["mask"])
	}

	if got := payload["model"]; got != "doubao-seedream-4-0-250828" {
		t.Fatalf("unexpected model: %#v", got)
	}
	if got := payload["prompt"]; got != "edit this image" {
		t.Fatalf("unexpected prompt: %#v", got)
	}
}

func TestConvertImageRequestEditsMultipartArrayFieldsNotDuplicated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "doubao-seedream-4-0-250828"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.WriteField("prompt", "edit array images"); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}

	for i := 0; i < 2; i++ {
		imagePart, err := writer.CreateFormFile("image[]", "test.png")
		if err != nil {
			t.Fatalf("create image part: %v", err)
		}
		if _, err = imagePart.Write([]byte("fake image bytes")); err != nil {
			t.Fatalf("write image part: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/images/edits", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertImageRequest(ctx, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}, dto.ImageRequest{
		Model:  "doubao-seedream-4-0-250828",
		Prompt: "edit array images",
	})
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	payload, ok := converted.(map[string]any)
	if !ok {
		t.Fatalf("unexpected converted type %T", converted)
	}

	imageValues, ok := payload["image"].([]string)
	if !ok {
		t.Fatalf("expected []string image payload, got %T", payload["image"])
	}
	if len(imageValues) != 2 {
		t.Fatalf("expected 2 images without duplication, got %d", len(imageValues))
	}
	for _, got := range imageValues {
		if !strings.HasPrefix(got, "data:text/plain; charset=utf-8;base64,") {
			t.Fatalf("expected image data url, got %#v", got)
		}
	}
}
