package volcengine

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

const (
	contextKeyTTSRequest     = "volcengine_tts_request"
	contextKeyResponseFormat = "response_format"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.ClaudeRequest) (any, error) {
	if _, ok := channelconstant.ChannelSpecialBases[info.ChannelBaseUrl]; ok {
		adaptor := claude.Adaptor{}
		return adaptor.ConvertClaudeRequest(c, info, req)
	}
	adaptor := openai.Adaptor{}
	return adaptor.ConvertClaudeRequest(c, info, req)
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	if info.RelayMode != constant.RelayModeAudioSpeech {
		return nil, errors.New("unsupported audio relay mode")
	}

	appID, token, err := parseVolcengineAuth(info.ApiKey)
	if err != nil {
		return nil, err
	}

	voiceType := mapVoiceType(request.Voice)
	speedRatio := lo.FromPtrOr(request.Speed, 0.0)
	encoding := mapEncoding(request.ResponseFormat)

	c.Set(contextKeyResponseFormat, encoding)

	volcRequest := VolcengineTTSRequest{
		App: VolcengineTTSApp{
			AppID:   appID,
			Token:   token,
			Cluster: "volcano_tts",
		},
		User: VolcengineTTSUser{
			UID: "openai_relay_user",
		},
		Audio: VolcengineTTSAudio{
			VoiceType:  voiceType,
			Encoding:   encoding,
			SpeedRatio: speedRatio,
			Rate:       24000,
		},
		Request: VolcengineTTSReqInfo{
			ReqID:     generateRequestID(),
			Text:      request.Input,
			Operation: "submit",
			Model:     info.OriginModelName,
		},
	}

	if len(request.Metadata) > 0 {
		if err = common.Unmarshal(request.Metadata, &volcRequest); err != nil {
			return nil, fmt.Errorf("error unmarshalling metadata to volcengine request: %w", err)
		}
	}

	c.Set(contextKeyTTSRequest, volcRequest)

	if volcRequest.Request.Operation == "submit" {
		info.IsStream = true
	}

	jsonData, err := common.Marshal(volcRequest)
	if err != nil {
		return nil, fmt.Errorf("error marshalling volcengine request: %w", err)
	}

	return bytes.NewReader(jsonData), nil
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	switch info.RelayMode {
	case constant.RelayModeImagesGenerations:
		return request, nil
	case constant.RelayModeImagesEdits:
		return a.convertImageEditRequest(c, request)

	default:
		return request, nil
	}
}

func (a *Adaptor) convertImageEditRequest(c *gin.Context, request dto.ImageRequest) (any, error) {
	if !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		return request, nil
	}

	mf := c.Request.MultipartForm
	if mf == nil {
		if _, err := c.MultipartForm(); err != nil {
			return nil, fmt.Errorf("failed to parse image edit form request: %w", err)
		}
		mf = c.Request.MultipartForm
	}
	if mf == nil {
		return nil, errors.New("no multipart form data found")
	}

	payload := make(map[string]any)
	appendImageRequestFields(payload, request)
	appendFormValues(payload, mf.Value)

	imageFiles := collectMultipartFiles(mf, "image")
	if len(imageFiles) == 0 {
		return nil, errors.New("image is required")
	}

	imageValue, err := fileHeadersToImageValue(imageFiles)
	if err != nil {
		return nil, err
	}
	payload["image"] = imageValue

	maskFiles := collectMultipartFiles(mf, "mask")
	if len(maskFiles) > 0 {
		maskValue, err := fileHeadersToImageValue(maskFiles[:1])
		if err != nil {
			return nil, fmt.Errorf("failed to read mask file: %w", err)
		}
		payload["mask"] = maskValue
	}

	return payload, nil
}

func appendImageRequestFields(payload map[string]any, request dto.ImageRequest) {
	if request.Model != "" {
		payload["model"] = request.Model
	}
	if request.Prompt != "" {
		payload["prompt"] = request.Prompt
	}
	if request.N != nil {
		payload["n"] = *request.N
	}
	if request.Size != "" {
		payload["size"] = request.Size
	}
	if request.Quality != "" {
		payload["quality"] = request.Quality
	}
	if request.ResponseFormat != "" {
		payload["response_format"] = request.ResponseFormat
	}
	if request.Watermark != nil {
		payload["watermark"] = *request.Watermark
	}

	appendRawJSONField(payload, "style", request.Style)
	appendRawJSONField(payload, "user", request.User)
	appendRawJSONField(payload, "extra_fields", request.ExtraFields)
	appendRawJSONField(payload, "background", request.Background)
	appendRawJSONField(payload, "moderation", request.Moderation)
	appendRawJSONField(payload, "output_format", request.OutputFormat)
	appendRawJSONField(payload, "output_compression", request.OutputCompression)
	appendRawJSONField(payload, "partial_images", request.PartialImages)
	appendRawJSONField(payload, "watermark_enabled", request.WatermarkEnabled)
	appendRawJSONField(payload, "user_id", request.UserId)
}

func appendRawJSONField(payload map[string]any, key string, raw []byte) {
	if len(raw) == 0 {
		return
	}

	var value any
	if err := common.Unmarshal(raw, &value); err != nil {
		return
	}
	payload[key] = value
}

func appendFormValues(payload map[string]any, values map[string][]string) {
	for key, items := range values {
		if len(items) == 0 {
			continue
		}
		switch key {
		case "image", "image[]", "mask", "model", "prompt", "n", "size", "quality", "response_format", "watermark":
			continue
		}
		if len(items) == 1 {
			payload[key] = items[0]
			continue
		}
		payload[key] = items
	}
}

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

func fileHeadersToImageValue(fileHeaders []*multipart.FileHeader) (any, error) {
	imageValues := make([]string, 0, len(fileHeaders))
	for _, fileHeader := range fileHeaders {
		dataURL, err := fileHeaderToDataURL(fileHeader)
		if err != nil {
			return nil, err
		}
		imageValues = append(imageValues, dataURL)
	}

	if len(imageValues) == 1 {
		return imageValues[0], nil
	}
	return imageValues, nil
}

func fileHeaderToDataURL(fileHeader *multipart.FileHeader) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open image file %q: %w", fileHeader.Filename, err)
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read image file %q: %w", fileHeader.Filename, err)
	}
	if len(fileBytes) == 0 {
		return "", fmt.Errorf("image file %q is empty", fileHeader.Filename)
	}

	mimeType := http.DetectContentType(fileBytes)
	base64Data := base64.StdEncoding.EncodeToString(fileBytes)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data), nil
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseUrl := info.ChannelBaseUrl
	if baseUrl == "" {
		baseUrl = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine]
	}
	specialPlan, hasSpecialPlan := channelconstant.ChannelSpecialBases[baseUrl]

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		if hasSpecialPlan && specialPlan.ClaudeBaseURL != "" {
			return fmt.Sprintf("%s/v1/messages", specialPlan.ClaudeBaseURL), nil
		}
		if strings.HasPrefix(info.UpstreamModelName, "bot") {
			return fmt.Sprintf("%s/api/v3/bots/chat/completions", baseUrl), nil
		}
		return fmt.Sprintf("%s/api/v3/chat/completions", baseUrl), nil
	default:
		switch info.RelayMode {
		case constant.RelayModeChatCompletions:
			if hasSpecialPlan && specialPlan.OpenAIBaseURL != "" {
				return fmt.Sprintf("%s/chat/completions", specialPlan.OpenAIBaseURL), nil
			}
			if strings.HasPrefix(info.UpstreamModelName, "bot") {
				return fmt.Sprintf("%s/api/v3/bots/chat/completions", baseUrl), nil
			}
			return fmt.Sprintf("%s/api/v3/chat/completions", baseUrl), nil
		case constant.RelayModeEmbeddings:
			return fmt.Sprintf("%s/api/v3/embeddings", baseUrl), nil
		//豆包的图生图也走generations接口: https://www.volcengine.com/docs/82379/1824121
		case constant.RelayModeImagesGenerations, constant.RelayModeImagesEdits:
			return fmt.Sprintf("%s/api/v3/images/generations", baseUrl), nil
		//case constant.RelayModeImagesEdits:
		//	return fmt.Sprintf("%s/api/v3/images/edits", baseUrl), nil
		case constant.RelayModeRerank:
			return fmt.Sprintf("%s/api/v3/rerank", baseUrl), nil
		case constant.RelayModeResponses:
			return fmt.Sprintf("%s/api/v3/responses", baseUrl), nil
		case constant.RelayModeAudioSpeech:
			if baseUrl == channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine] {
				return "wss://openspeech.bytedance.com/api/v1/tts/ws_binary", nil
			}
			return fmt.Sprintf("%s/v1/audio/speech", baseUrl), nil
		default:
		}
	}
	return "", fmt.Errorf("unsupported relay mode: %d", info.RelayMode)
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)

	if info.RelayMode == constant.RelayModeAudioSpeech {
		parts := strings.Split(info.ApiKey, "|")
		if len(parts) == 2 {
			req.Set("Authorization", "Bearer;"+parts[1])
		}
		req.Set("Content-Type", "application/json")
		return nil
	} else if info.RelayMode == constant.RelayModeImagesEdits {
		req.Set("Content-Type", gin.MIMEJSON)
	}

	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}

	if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) &&
		strings.HasSuffix(info.UpstreamModelName, "-thinking") &&
		strings.HasPrefix(info.UpstreamModelName, "deepseek") {
		info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-thinking")
		request.Model = info.UpstreamModelName
		request.THINKING = json.RawMessage(`{"type": "enabled"}`)
	}
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayMode == constant.RelayModeAudioSpeech {
		baseUrl := info.ChannelBaseUrl
		if baseUrl == "" {
			baseUrl = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine]
		}

		if baseUrl == channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine] {
			if info.IsStream {
				return nil, nil
			}
		}
	}
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.RelayFormat == types.RelayFormatClaude {
		if _, ok := channelconstant.ChannelSpecialBases[info.ChannelBaseUrl]; ok {
			adaptor := claude.Adaptor{}
			return adaptor.DoResponse(c, resp, info)
		}
	}

	if info.RelayMode == constant.RelayModeAudioSpeech {
		encoding := mapEncoding(c.GetString(contextKeyResponseFormat))
		if info.IsStream {
			volcRequestInterface, exists := c.Get(contextKeyTTSRequest)
			if !exists {
				return nil, types.NewErrorWithStatusCode(
					errors.New("volcengine TTS request not found in context"),
					types.ErrorCodeBadRequestBody,
					http.StatusInternalServerError,
				)
			}

			volcRequest, ok := volcRequestInterface.(VolcengineTTSRequest)
			if !ok {
				return nil, types.NewErrorWithStatusCode(
					errors.New("invalid volcengine TTS request type"),
					types.ErrorCodeBadRequestBody,
					http.StatusInternalServerError,
				)
			}

			// Get the WebSocket URL
			requestURL, urlErr := a.GetRequestURL(info)
			if urlErr != nil {
				return nil, types.NewErrorWithStatusCode(
					urlErr,
					types.ErrorCodeBadRequestBody,
					http.StatusInternalServerError,
				)
			}
			return handleTTSWebSocketResponse(c, requestURL, volcRequest, info, encoding)
		}
		return handleTTSResponse(c, resp, info, encoding)
	}

	adaptor := openai.Adaptor{}
	usage, err = adaptor.DoResponse(c, resp, info)
	return
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
