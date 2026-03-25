# Volcengine `/v1/images/edits` Fix Migration Notes

## Purpose

This document packages the Doubao / Volcengine image edit issue into a form suitable for:

1. filing issues against the upstream `new-api` repository;
2. replaying the same fix in the latest upstream codebase;
3. preparing a PR with a clear change log and verification steps.

The current fork already contains the working fix in:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go)
- [relay/channel/volcengine/adaptor_test.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor_test.go)

## Issue 1: Volcengine `/v1/images/edits` loses uploaded source image

### Suggested title

`Volcengine channel: /v1/images/edits drops multipart image payload when forwarding to /api/v3/images/generations`

### Suggested issue body

#### Summary

When using the Volcengine / Doubao channel, requests to `/v1/images/edits` accept multipart form uploads from the client, but the gateway does not convert uploaded files into the JSON image payload expected by Volcengine. The upstream request is sent as JSON, but the original image content is missing, so image edit requests fail.

#### Symptoms

- `POST /v1/images/edits` reaches the Volcengine adaptor.
- The gateway forwards the request to `/api/v3/images/generations`.
- The forwarded JSON payload does not include the uploaded source image bytes converted to a usable string value.
- Doubao image editing fails even though the route exists.

#### Root cause

The Volcengine adaptor had no effective multipart-to-JSON conversion path for image edits.

In the fixed fork, the relevant logic is now implemented at:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L122)

Previously:

- multipart files were not transformed into Volcengine-compatible image strings;
- the adaptor effectively returned the partially parsed request object;
- the request was then sent as JSON without the actual source image content.

#### Expected behavior

For Volcengine image edit requests:

- read multipart `image` uploads from `MultipartForm.File`;
- optionally read `mask`;
- convert uploaded files into `data:` URLs or another Volcengine-supported image string format;
- send the final JSON body to `/api/v3/images/generations`.

#### Reproduction sketch

```bash
curl http://127.0.0.1:3000/v1/images/edits \
  -H "Authorization: Bearer <token>" \
  -F "model=<doubao-model>" \
  -F "prompt=edit this image" \
  -F "image=@/absolute/path/test.png"
```

#### Fix summary

Add a dedicated `RelayModeImagesEdits` conversion branch in the Volcengine adaptor that:

- parses multipart form data;
- collects image and mask files;
- converts each file to a `data:` URL;
- builds a JSON payload with `model`, `prompt`, `n`, `size`, `quality`, `response_format`, `watermark`, `image`, and `mask`.

## Issue 2: `image[]` uploads may be duplicated if file collection mixes explicit and prefix matching

### Suggested title

`Volcengine channel: image[] multipart fields can be collected twice during /v1/images/edits conversion`

### Suggested issue body

#### Summary

When collecting multipart files for Volcengine image edits, array-style form fields such as `image[]` or `mask[]` must be collected exactly once. If the collector first appends `field+"[]"` and then also scans for all keys with prefix `field+"["`, the same file entries are collected twice.

#### Impact

- a single uploaded `image[]` file may become two output image strings;
- multi-image requests can be duplicated silently;
- upstream behavior changes unexpectedly or may fail validation.

#### Expected behavior

The file collector should do a single pass over multipart file keys and include:

- `image`
- `image[]`
- `image[0]`, `image[1]`, etc.

Each uploaded file should be added exactly once.

#### Fix summary

Use a single-pass collector:

```go
func collectMultipartFiles(mf *multipart.Form, field string) []*multipart.FileHeader {
    if mf == nil || mf.File == nil {
        return nil
    }

    var files []*multipart.FileHeader
    for fieldName, headers := range mf.File {
        if fieldName == field || fieldName == field+"[]" || strings.HasPrefix(fieldName, field+"[") {
            files = append(files, headers...)
        }
    }
    return files
}
```

## Porting guide for the upstream latest repository

### Target area

Find the Volcengine adaptor in the latest upstream repo, typically:

- `relay/channel/volcengine/adaptor.go`

Also locate or add:

- `relay/channel/volcengine/adaptor_test.go`

### Change set

#### 1. Implement image edit conversion

In `ConvertImageRequest`, add a `RelayModeImagesEdits` branch that calls a dedicated helper.

Reference from this fork:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L110)

Desired shape:

```go
case constant.RelayModeImagesEdits:
    return a.convertImageEditRequest(c, request)
```

#### 2. Add multipart-to-JSON conversion helper

Add a helper similar to:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L122)

Required behavior:

- if request is not multipart, return the request unchanged;
- parse `c.MultipartForm()` when needed;
- collect uploaded `image` files;
- collect optional `mask` file;
- fail early if no image file is present;
- convert uploaded files to `data:` URLs;
- return a JSON-serializable map payload.

#### 3. Preserve typed request fields in the outgoing JSON payload

Build the payload using already validated request fields:

- `model`
- `prompt`
- `n`
- `size`
- `quality`
- `response_format`
- `watermark`

Reference:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L165)

#### 4. Preserve raw JSON fields where possible

If the latest upstream still uses `dto.ImageRequest` raw fields, carry over:

- `style`
- `user`
- `extra_fields`
- `background`
- `moderation`
- `output_format`
- `output_compression`
- `partial_images`
- `watermark_enabled`
- `user_id`

Reference:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L188)

#### 5. Add file collection helper without duplication

Port the collector logic from:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L229)

Important detail:

- do not combine two collection strategies that both match `image[]`;
- use a single-pass scan of multipart keys.

#### 6. Add data URL conversion helper

Port the file-to-data-URL helper from:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L243)

Required behavior:

- open each uploaded file;
- read file bytes;
- reject empty files;
- infer MIME type with `http.DetectContentType`;
- base64-encode file bytes;
- return `data:<mime>;base64,<payload>`.

#### 7. Keep JSON encoding consistent with project wrappers

In this fork, the touched Volcengine adaptor also aligned audio request JSON operations to `common.Marshal` / `common.Unmarshal`.

Relevant lines:

- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L88)
- [relay/channel/volcengine/adaptor.go](/Users/lkg/code/00AES/yaddas/new-api/relay/channel/volcengine/adaptor.go#L102)

If the latest upstream file still uses `encoding/json` marshal/unmarshal calls in touched business logic, update them to the common wrappers where appropriate.

## Suggested PR description

### Title

`fix(volcengine): support multipart /v1/images/edits payload conversion`

### Summary

- add Volcengine image edit multipart conversion for `/v1/images/edits`
- convert uploaded image and mask files to data URLs before forwarding
- avoid duplicate collection of `image[]` multipart fields
- add unit tests for single-image and array-style multipart inputs

### Detailed change log

1. Added `RelayModeImagesEdits` handling in the Volcengine image adaptor.
2. Added helper functions to:
   - parse multipart form uploads;
   - collect `image`, `image[]`, and indexed `image[n]` file fields;
   - collect optional `mask`;
   - convert uploaded files into `data:` URLs.
3. Preserved typed request fields and selected raw JSON fields in the forwarded payload.
4. Added tests covering:
   - single `image` + `mask` multipart conversion;
   - `image[]` array fields without duplication.

## Verification checklist

Use these steps in the upstream latest repo after porting the patch.

### Unit tests

```bash
go test ./relay/channel/volcengine
go test ./relay/...
```

### Manual smoke test

```bash
curl http://127.0.0.1:3000/v1/images/edits \
  -H "Authorization: Bearer <token>" \
  -F "model=<doubao-model>" \
  -F "prompt=edit this image" \
  -F "image=@/absolute/path/test.png"
```

### Array field smoke test

```bash
curl http://127.0.0.1:3000/v1/images/edits \
  -H "Authorization: Bearer <token>" \
  -F "model=<doubao-model>" \
  -F "prompt=edit these images" \
  -F "image[]=@/absolute/path/a.png" \
  -F "image[]=@/absolute/path/b.png"
```

Expected result:

- upstream receives JSON with `image` populated;
- array-style uploads are not duplicated;
- image edit no longer fails because source image content is missing.

## Notes

- The initial failure was not a Doubao upstream capability issue; it was an adaptor gap in the gateway.
- The duplicate `image[]` issue appeared during review of the first fix and should be included in the upstream PR to avoid follow-up regressions.
