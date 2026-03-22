---
summary: "Anthropic Go SDK reference - Claude API, messages, tools, documents, structured output"
read_when: [AI integration, Claude API calls, document processing, risk classification]
---

# Anthropic Go SDK Reference

Import path: `github.com/anthropics/anthropic-sdk-go`

Requires Go 1.22+.

```sh
go get -u github.com/anthropics/anthropic-sdk-go@v1.27.1
```

---

## 1. Client Setup

The SDK reads `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`, and `ANTHROPIC_AUTH_TOKEN` from environment variables automatically.

```go
import (
    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

// Minimal — reads ANTHROPIC_API_KEY from env
client := anthropic.NewClient()

// Explicit API key
client := anthropic.NewClient(
    option.WithAPIKey("sk-ant-..."),
)

// Custom base URL (e.g., proxy or on-prem)
client := anthropic.NewClient(
    option.WithBaseURL("https://my-proxy.example.com"),
)

// With retry and timeout configuration
client := anthropic.NewClient(
    option.WithMaxRetries(3),           // default is 2 retries
    option.WithRequestTimeout(30*time.Second),
)

// Additional options
client := anthropic.NewClient(
    option.WithHeader("X-Custom", "value"),  // custom headers
    option.WithMiddleware(myMiddleware),       // request interceptors
)
```

### Client Structure

```go
type Client struct {
    Options     []option.RequestOption
    Completions CompletionService
    Messages    MessageService   // Messages.New(), Messages.NewStreaming(), Messages.Batches
    Models      ModelService
    Beta        BetaService
}
```

---

## 2. Messages API

### Basic Request

```go
message, err := client.Messages.New(context.TODO(), anthropic.MessageNewParams{
    MaxTokens: 1024,
    Model:     anthropic.ModelClaudeSonnet4_6,
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock("What is a quaternion?")),
    },
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(message.Content[0].AsAny().(anthropic.TextBlock).Text)
```

### Model Constants

```go
anthropic.ModelClaudeOpus4_6              // "claude-opus-4-6"
anthropic.ModelClaudeSonnet4_6            // "claude-sonnet-4-6"
anthropic.ModelClaudeHaiku4_5             // "claude-haiku-4-5"
anthropic.ModelClaudeOpus4_5              // "claude-opus-4-5"
anthropic.ModelClaudeSonnet4_5            // "claude-sonnet-4-5"
anthropic.ModelClaudeSonnet4_5_20250929   // "claude-sonnet-4-5-20250929"
anthropic.ModelClaudeOpus4_1              // "claude-opus-4-1"
anthropic.ModelClaudeOpus4_0              // "claude-opus-4-0"
anthropic.ModelClaudeSonnet4_0            // "claude-sonnet-4-0"
anthropic.ModelClaude_3_Haiku_20240307    // deprecated, use claude-haiku-4-5
```

### System Prompt

```go
message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    MaxTokens: 2048,
    Model:     anthropic.ModelClaudeSonnet4_6,
    System: []anthropic.TextBlockParam{
        {Text: "You are an insurance underwriting assistant. Analyze risk factors precisely."},
    },
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock("Classify the risk level of this property.")),
    },
})
```

### Multi-Turn Conversation

```go
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("Hello, I need help with a claim.")),
    anthropic.NewAssistantMessage(anthropic.NewTextBlock("I'd be happy to help. What type of claim?")),
    anthropic.NewUserMessage(anthropic.NewTextBlock("It's an auto insurance claim from last week.")),
}

message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    MaxTokens: 1024,
    Model:     anthropic.ModelClaudeSonnet4_6,
    Messages:  messages,
})
```

### MessageNewParams Full Reference

```go
type MessageNewParams struct {
    MaxTokens     int64                     // required
    Messages      []MessageParam            // required
    Model         Model                     // required
    Temperature   param.Opt[float64]        // 0.0-1.0, default 1.0
    TopK          param.Opt[int64]
    TopP          param.Opt[float64]
    CacheControl  CacheControlEphemeralParam
    Metadata      MetadataParam
    OutputConfig  OutputConfigParam         // structured outputs / effort
    ServiceTier   MessageNewParamsServiceTier
    StopSequences []string
    System        []TextBlockParam
    Thinking      ThinkingConfigParamUnion
    ToolChoice    ToolChoiceUnionParam
    Tools         []ToolUnionParam
}
```

### Response Object

```go
// message.Content is []ContentBlock — use .AsAny() type switch
for _, block := range message.Content {
    switch v := block.AsAny().(type) {
    case anthropic.TextBlock:
        fmt.Println(v.Text)
    case anthropic.ToolUseBlock:
        fmt.Printf("Tool: %s, Input: %v\n", v.Name, v.Input)
    }
}

// Other response fields:
message.ID            // "msg_..."
message.Model         // model used
message.StopReason    // "end_turn", "stop_sequence", "tool_use", "max_tokens"
message.StopSequence  // matched stop sequence (if any)
message.Usage.InputTokens
message.Usage.OutputTokens
```

---

## 3. Structured Outputs

### JSON Schema via OutputConfig (GA)

Use `OutputConfig` with a `JSONOutputFormatParam` to force schema-compliant JSON responses.

```go
message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    MaxTokens: 1024,
    Model:     anthropic.ModelClaudeSonnet4_6,
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock(
            "Extract info: John Smith (john@example.com) wants Enterprise plan, demo Tuesday 2pm.",
        )),
    },
    OutputConfig: anthropic.OutputConfigParam{
        Format: anthropic.JSONOutputFormatParam{
            Schema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "name":           map[string]any{"type": "string"},
                    "email":          map[string]any{"type": "string"},
                    "plan_interest":  map[string]any{"type": "string"},
                    "demo_requested": map[string]any{"type": "boolean"},
                },
                "required":             []string{"name", "email", "plan_interest", "demo_requested"},
                "additionalProperties": false,
            },
        },
    },
})
// Response: message.Content[0] is TextBlock with valid JSON string
```

### Schema Generation from Go Structs

Use `github.com/invopop/jsonschema` to generate schemas from Go structs:

```go
import "github.com/invopop/jsonschema"

type RiskAssessment struct {
    RiskLevel   string   `json:"risk_level" jsonschema:"enum=low,enum=medium,enum=high,enum=critical"`
    Score       int      `json:"score" jsonschema:"minimum=0,maximum=100"`
    Factors     []string `json:"factors" jsonschema:"minItems=1,maxItems=10"`
    Recommended bool     `json:"recommended"`
    Notes       string   `json:"notes,omitempty"`
}

func generateJSONSchema(v any) map[string]any {
    reflector := jsonschema.Reflector{
        AllowAdditionalProperties: false,
        DoNotReference:            true,
    }
    schema := reflector.Reflect(v)
    b, _ := json.Marshal(schema)
    var m map[string]any
    json.Unmarshal(b, &m)
    return m
}

schemaMap := generateJSONSchema(&RiskAssessment{})

// Use with Beta API for structured-outputs beta
msg, err := client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
    Model:        anthropic.Model("claude-sonnet-4-6"),
    MaxTokens:    1024,
    Messages:     []anthropic.BetaMessageParam{
        anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("Assess risk for a coastal property in Florida.")),
    },
    OutputFormat: anthropic.BetaJSONSchemaOutputFormat(schemaMap),
    Betas:        []anthropic.AnthropicBeta{"structured-outputs-2025-11-13"},
})
```

### Effort Level

Control response quality vs. speed:

```go
OutputConfig: anthropic.OutputConfigParam{
    Effort: "low",   // "low" | "medium" | "high" | "max"
},
```

---

## 4. Tool Use

### Defining Tools

```go
toolParams := []anthropic.ToolParam{
    {
        Name:        "get_policy_details",
        Description: anthropic.String("Look up insurance policy details by policy number"),
        InputSchema: anthropic.ToolInputSchemaParam{
            Properties: map[string]any{
                "policy_number": map[string]any{
                    "type":        "string",
                    "description": "The policy number, e.g. POL-2024-001",
                },
                "include_claims": map[string]any{
                    "type":        "boolean",
                    "description": "Whether to include claims history",
                },
            },
            Required: []string{"policy_number"},
        },
    },
    {
        Name:        "calculate_premium",
        Description: anthropic.String("Calculate insurance premium based on risk factors"),
        InputSchema: anthropic.ToolInputSchemaParam{
            Properties: map[string]any{
                "coverage_type": map[string]any{
                    "type": "string",
                    "enum": []string{"auto", "home", "life", "commercial"},
                },
                "risk_score": map[string]any{
                    "type":        "number",
                    "description": "Risk score from 0-100",
                },
            },
            Required: []string{"coverage_type", "risk_score"},
        },
    },
}

// Wrap in ToolUnionParam
tools := make([]anthropic.ToolUnionParam, len(toolParams))
for i, tp := range toolParams {
    tools[i] = anthropic.ToolUnionParam{OfTool: &tp}
}
```

### Strict Tool Use

Add `Strict: param.Opt[bool]` to guarantee schema-valid tool inputs:

```go
{
    Name:        "get_policy_details",
    Description: anthropic.String("Look up policy details"),
    Strict:      param.NewOpt(true),
    InputSchema: anthropic.ToolInputSchemaParam{
        Properties: map[string]any{...},
        Required:   []string{"policy_number"},
    },
}
```

### Tool Choice

```go
// Auto (default) — model decides
ToolChoice: anthropic.ToolChoiceUnionParam{
    OfAuto: &anthropic.ToolChoiceAutoParam{},
}

// Any — must use at least one tool
ToolChoice: anthropic.ToolChoiceUnionParam{
    OfAny: &anthropic.ToolChoiceAnyParam{},
}

// Specific tool — force a particular tool
ToolChoice: anthropic.ToolChoiceParamOfTool("get_policy_details"),

// None — no tool use allowed
ToolChoice: anthropic.ToolChoiceUnionParam{
    OfNone: &anthropic.ToolChoiceNoneParam{Type: "none"},
}

// Disable parallel tool use (one tool call at a time)
ToolChoice: anthropic.ToolChoiceUnionParam{
    OfAuto: &anthropic.ToolChoiceAutoParam{
        DisableParallelToolUse: param.NewOpt(true),
    },
}
```

### Multi-Turn Tool Use Loop

```go
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("What is the premium for policy POL-2024-001?")),
}

for {
    message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.ModelClaudeSonnet4_6,
        MaxTokens: 1024,
        Messages:  messages,
        Tools:     tools,
    })
    if err != nil {
        log.Fatal(err)
    }

    // Add assistant response to conversation
    messages = append(messages, message.ToParam())

    // Collect tool results
    var toolResults []anthropic.ContentBlockParamUnion
    for _, block := range message.Content {
        switch v := block.AsAny().(type) {
        case anthropic.TextBlock:
            fmt.Println(v.Text)
        case anthropic.ToolUseBlock:
            // Parse input and call your function
            var result string
            switch block.Name {
            case "get_policy_details":
                var input struct {
                    PolicyNumber  string `json:"policy_number"`
                    IncludeClaims bool   `json:"include_claims"`
                }
                json.Unmarshal([]byte(v.JSON.Input.Raw()), &input)
                result = lookupPolicy(input.PolicyNumber, input.IncludeClaims)
            case "calculate_premium":
                var input struct {
                    CoverageType string  `json:"coverage_type"`
                    RiskScore    float64 `json:"risk_score"`
                }
                json.Unmarshal([]byte(v.JSON.Input.Raw()), &input)
                result = calculatePremium(input.CoverageType, input.RiskScore)
            }
            toolResults = append(toolResults,
                anthropic.NewToolResultBlock(block.ID, result, false),
            )
        }
    }

    // If no tool calls, we're done
    if len(toolResults) == 0 {
        break
    }

    // Send tool results back
    messages = append(messages, anthropic.NewUserMessage(toolResults...))
}
```

---

## 5. PDF / Document Processing

### Supported Formats and Limits

| Requirement | Limit |
|---|---|
| Max request size | 32 MB |
| Max pages per request | 600 (100 for 200k-context models) |
| Format | Standard PDF (no passwords/encryption) |

Token cost per page: ~1,500-3,000 text tokens + image tokens (each page rendered as image).

### Base64-Encoded PDF

```go
import (
    "encoding/base64"
    "os"
)

pdfBytes, _ := os.ReadFile("policy-document.pdf")
pdfBase64 := base64.StdEncoding.EncodeToString(pdfBytes)

message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    MaxTokens: 2048,
    Model:     anthropic.ModelClaudeSonnet4_6,
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(
            anthropic.ContentBlockParamUnion{
                OfDocument: &anthropic.DocumentBlockParam{
                    Source: anthropic.DocumentBlockParamSourceUnion{
                        OfBase64: &anthropic.Base64PDFSourceParam{
                            Data: pdfBase64,
                        },
                    },
                },
            },
            anthropic.NewTextBlock("Extract all coverage limits and deductibles from this policy."),
        ),
    },
})
```

### URL-Based PDF

```go
message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    MaxTokens: 2048,
    Model:     anthropic.ModelClaudeSonnet4_6,
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(
            anthropic.ContentBlockParamUnion{
                OfDocument: &anthropic.DocumentBlockParam{
                    Source: anthropic.DocumentBlockParamSourceUnion{
                        OfURL: &anthropic.URLPDFSourceParam{
                            URL: "https://example.com/policy-document.pdf",
                        },
                    },
                },
            },
            anthropic.NewTextBlock("What are the key findings in this document?"),
        ),
    },
})
```

### File API (for Repeated Use)

Upload once, reference by `file_id` to avoid re-encoding:

```go
// Upload via Files API (beta), then reference:
anthropic.ContentBlockParamUnion{
    OfDocument: &anthropic.DocumentBlockParam{
        Source: anthropic.DocumentBlockParamSourceUnion{
            OfFile: &anthropic.FileDocumentSourceParam{
                FileID: "file_abc123",
            },
        },
    },
}
```

### Best Practices for PDFs

- Place PDFs before text in messages
- Use standard fonts; ensure text is legible
- Rotate pages to proper upright orientation
- Split large PDFs into chunks
- Enable prompt caching for repeated analysis
- Use logical page numbers from PDF viewer when referencing

---

## 6. Streaming

### Basic Streaming

```go
stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
    MaxTokens: 1024,
    Model:     anthropic.ModelClaudeSonnet4_6,
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock("Explain insurance underwriting.")),
    },
})

for stream.Next() {
    event := stream.Current()

    switch ev := event.AsAny().(type) {
    case anthropic.MessageStartEvent:
        fmt.Printf("Message started, model: %s\n", ev.Message.Model)
    case anthropic.ContentBlockDeltaEvent:
        switch delta := ev.Delta.AsAny().(type) {
        case anthropic.TextDelta:
            fmt.Print(delta.Text)  // incremental text
        }
    case anthropic.MessageDeltaEvent:
        fmt.Printf("\nStop reason: %s\n", ev.Delta.StopReason)
        fmt.Printf("Output tokens: %d\n", ev.Usage.OutputTokens)
    }
}

if stream.Err() != nil {
    log.Fatal(stream.Err())
}
```

### Streaming Event Types

```go
// Main events from MessageStreamEventUnion.AsAny():
anthropic.MessageStartEvent        // message metadata
anthropic.ContentBlockStartEvent   // new content block begins
anthropic.ContentBlockDeltaEvent   // incremental content (text, tool input)
anthropic.ContentBlockStopEvent    // content block complete
anthropic.MessageDeltaEvent        // stop_reason, usage
anthropic.MessageStopEvent         // message complete

// Delta variants from ContentBlockDeltaEvent.Delta.AsAny():
anthropic.TextDelta         // .Text string — partial text
anthropic.InputJSONDelta    // .PartialJSON string — partial tool input
```

### Streaming with Tool Use

```go
stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
    MaxTokens: 1024,
    Model:     anthropic.ModelClaudeSonnet4_6,
    Messages:  messages,
    Tools:     tools,
})

var fullJSON strings.Builder

for stream.Next() {
    event := stream.Current()
    switch ev := event.AsAny().(type) {
    case anthropic.ContentBlockDeltaEvent:
        switch delta := ev.Delta.AsAny().(type) {
        case anthropic.TextDelta:
            fmt.Print(delta.Text)
        case anthropic.InputJSONDelta:
            fullJSON.WriteString(delta.PartialJSON)
        }
    case anthropic.ContentBlockStopEvent:
        if fullJSON.Len() > 0 {
            fmt.Printf("Tool input: %s\n", fullJSON.String())
            fullJSON.Reset()
        }
    }
}
```

---

## 7. Batch Processing

For high-volume, async processing. Batches complete within 24 hours at 50% cost.

### Create a Batch

```go
batch, err := client.Messages.Batches.New(ctx, anthropic.MessageBatchNewParams{
    Requests: []anthropic.MessageBatchNewParamsRequest{
        {
            CustomID: "policy-review-001",
            Params: anthropic.MessageBatchNewParamsRequestParams{
                Model:     anthropic.ModelClaudeSonnet4_6,
                MaxTokens: 2048,
                Messages: []anthropic.MessageParam{
                    anthropic.NewUserMessage(
                        anthropic.NewTextBlock("Summarize this insurance policy document."),
                    ),
                },
            },
        },
        {
            CustomID: "policy-review-002",
            Params: anthropic.MessageBatchNewParamsRequestParams{
                Model:     anthropic.ModelClaudeSonnet4_6,
                MaxTokens: 2048,
                Messages: []anthropic.MessageParam{
                    anthropic.NewUserMessage(
                        anthropic.NewTextBlock("Extract all exclusion clauses."),
                    ),
                },
            },
        },
    },
})
fmt.Printf("Batch ID: %s, Status: %s\n", batch.ID, batch.ProcessingStatus)
```

### Check Batch Status

```go
batch, err := client.Messages.Batches.Get(ctx, "batch_1234567890abcdef")
fmt.Printf("Status: %s\n", batch.ProcessingStatus)
fmt.Printf("Succeeded: %d, Errored: %d, Processing: %d\n",
    batch.RequestCounts.Succeeded,
    batch.RequestCounts.Errored,
    batch.RequestCounts.Processing,
)
```

### Retrieve Results (JSONL Stream)

```go
results := client.Messages.Batches.ResultsStreaming(ctx, batch.ID)
for results.Next() {
    result := results.Current()
    fmt.Printf("ID: %s, Type: %s\n", result.CustomID, result.Result.Type)
    // result.Result.Message contains the full Message response
}
if results.Err() != nil {
    log.Fatal(results.Err())
}
```

### List and Cancel Batches

```go
// List batches
page, err := client.Messages.Batches.List(ctx, anthropic.MessageBatchListParams{})
for _, b := range page.Data {
    fmt.Printf("%s: %s\n", b.ID, b.ProcessingStatus)
}

// Cancel a batch
batch, err := client.Messages.Batches.Cancel(ctx, "batch_1234567890abcdef")
```

### Batch Lifecycle States

| State | Description |
|---|---|
| `processing` | Batch is being processed |
| `succeeded` | All requests completed (individual ones may have errored) |
| `failed` | Batch processing failed |
| `expired` | Not completed within 24 hours |
| `canceled` | Batch was canceled |

---

## 8. Prompt Caching

Reduces cost and latency for repeated prefixes. Cache hits cost 0.1x base input price.

### Automatic Caching (Simplest)

```go
message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    MaxTokens: 1024,
    Model:     anthropic.ModelClaudeSonnet4_6,
    CacheControl: anthropic.CacheControlEphemeralParam{},  // auto-places breakpoint
    System: []anthropic.TextBlockParam{
        {Text: "You are an insurance underwriting assistant with deep expertise..."},
    },
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock("Analyze this risk.")),
    },
})
```

### Explicit Cache Breakpoints

Place `CacheControl` on specific content blocks for fine-grained control (up to 4 breakpoints):

```go
message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    MaxTokens: 2048,
    Model:     anthropic.ModelClaudeSonnet4_6,
    System: []anthropic.TextBlockParam{
        {
            Text:         "You are an insurance document analyst.",
            CacheControl: anthropic.CacheControlEphemeralParam{},  // cache this
        },
    },
    Tools: tools,  // tool definitions are cached when breakpoint is on last tool
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(
            anthropic.ContentBlockParamUnion{
                OfDocument: &anthropic.DocumentBlockParam{
                    Source: anthropic.DocumentBlockParamSourceUnion{
                        OfBase64: &anthropic.Base64PDFSourceParam{
                            Data: largePDFBase64,
                        },
                    },
                    CacheControl: anthropic.CacheControlEphemeralParam{},  // cache PDF
                },
            },
            anthropic.NewTextBlock("Summarize the key coverage terms."),
        ),
    },
})
```

### Cache TTL Options

Default is 5-minute TTL. Extended 1-hour TTL costs 2x base input:

```go
// 1-hour cache (in raw JSON — set via option)
option.WithJSONSet("system.0.cache_control", map[string]any{
    "type": "ephemeral",
    "ttl":  "1h",
})
```

### Monitoring Cache Performance

```go
fmt.Printf("Cache read tokens:  %d\n", message.Usage.CacheReadInputTokens)
fmt.Printf("Cache write tokens: %d\n", message.Usage.CacheCreationInputTokens)
fmt.Printf("Uncached tokens:    %d\n", message.Usage.InputTokens)
```

### Minimum Cacheable Tokens

| Model | Min Tokens |
|---|---|
| Claude Opus 4.6, 4.5 | 4,096 |
| Claude Sonnet 4.6 | 2,048 |
| Claude Sonnet 4.5, 4.1, 4.0 | 1,024 |
| Claude Haiku 4.5 | 4,096 |

### Pricing (per MTok)

| Model | Base Input | Cache Write (5m) | Cache Write (1h) | Cache Hit | Output |
|---|---|---|---|---|---|
| Opus 4.6 | $5 | $6.25 | $10 | $0.50 | $25 |
| Sonnet 4.6 | $3 | $3.75 | $6 | $0.30 | $15 |
| Haiku 4.5 | $1 | $1.25 | $2 | $0.10 | $5 |

### What Invalidates Cache

- Changes to tool definitions
- Enabling/disabling web search or citations
- Changes to thinking parameters, images, or tool_choice

---

## 9. Error Handling

### Error Types

```go
import "github.com/anthropics/anthropic-sdk-go"

// Type aliases for API errors:
anthropic.Error                    // base error with StatusCode, Request, Response, RequestID
anthropic.InvalidRequestError      // 400 — malformed request
anthropic.AuthenticationError      // 401 — invalid API key
anthropic.PermissionError          // 403 — insufficient permissions
anthropic.NotFoundError            // 404
anthropic.RateLimitError           // 429 — rate limited
anthropic.OverloadedError          // 529 — API overloaded
anthropic.GatewayTimeoutError      // 504
anthropic.BillingError             // billing issue
```

### Checking Error Types

```go
message, err := client.Messages.New(ctx, params)
if err != nil {
    var apiErr *anthropic.Error
    if errors.As(err, &apiErr) {
        fmt.Printf("Status: %d\n", apiErr.StatusCode)
        fmt.Printf("Request ID: %s\n", apiErr.RequestID)
        fmt.Printf("Raw: %s\n", apiErr.RawJSON())

        // Dump request/response for debugging
        fmt.Printf("Request:\n%s\n", apiErr.DumpRequest(true))
        fmt.Printf("Response:\n%s\n", apiErr.DumpResponse(true))

        switch apiErr.StatusCode {
        case 429:
            // Rate limited — SDK auto-retries with backoff
            log.Println("Rate limited, retries exhausted")
        case 529:
            // Overloaded — back off and retry later
            log.Println("API overloaded")
        }
    }
    log.Fatal(err)
}
```

### Automatic Retries

The SDK automatically retries on:
- 408 Request Timeout
- 409 Conflict
- 429 Rate Limit (with backoff)
- 5xx Server Errors

Default: 2 retries. Configure with `option.WithMaxRetries(n)`. Set to 0 for no retries.

```go
// No retries
client := anthropic.NewClient(option.WithMaxRetries(0))

// More aggressive retry
client := anthropic.NewClient(option.WithMaxRetries(5))

// Per-request override
message, err := client.Messages.New(ctx, params, option.WithMaxRetries(3))
```

### Timeouts

Default non-streaming timeout is calculated based on `MaxTokens` and model. Override per request:

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

message, err := client.Messages.New(ctx, params)
```

Or via option:

```go
message, err := client.Messages.New(ctx, params,
    option.WithRequestTimeout(30*time.Second),
)
```

---

## 10. Best Practices

### Token Management

```go
// Count tokens before sending (avoid surprise costs)
count, err := client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
    Model:    anthropic.ModelClaudeSonnet4_6,
    Messages: messages,
    System:   systemPrompt,
    Tools:    tools,
})
fmt.Printf("Input tokens: %d\n", count.InputTokens)
```

### Cost Optimization

1. **Use Haiku for simple tasks** — 5x cheaper than Sonnet, 25x cheaper than Opus.
2. **Enable prompt caching** for repeated system prompts, tool definitions, or reference documents. Cache hits are 90% cheaper.
3. **Batch processing** for non-urgent workloads — 50% cost reduction.
4. **Set appropriate MaxTokens** — don't set 8192 when you expect 200 tokens.
5. **Use structured outputs** to avoid re-prompting on malformed responses.
6. **Temperature 0.0** for deterministic extraction tasks; reduces output variance.

### Insurance Domain: Risk Classification

```go
func classifyRisk(client anthropic.Client, applicationText string) (*RiskAssessment, error) {
    schemaMap := generateJSONSchema(&RiskAssessment{})

    message, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
        MaxTokens:   1024,
        Model:       anthropic.ModelClaudeSonnet4_6,
        Temperature: param.NewOpt(0.0),  // deterministic
        System: []anthropic.TextBlockParam{
            {Text: `You are an insurance risk classification engine. Analyze the application
and return a structured risk assessment. Consider: property location, construction type,
occupancy, claims history, credit score, and proximity to fire/flood zones.
Risk levels: low (score 0-25), medium (26-50), high (51-75), critical (76-100).`},
        },
        OutputConfig: anthropic.OutputConfigParam{
            Format: anthropic.JSONOutputFormatParam{
                Schema: schemaMap,
            },
        },
        Messages: []anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock(applicationText)),
        },
    })
    if err != nil {
        return nil, err
    }

    var result RiskAssessment
    text := message.Content[0].AsAny().(anthropic.TextBlock).Text
    if err := json.Unmarshal([]byte(text), &result); err != nil {
        return nil, fmt.Errorf("parse response: %w", err)
    }
    return &result, nil
}
```

### Insurance Domain: Document Extraction

```go
func extractPolicyData(client anthropic.Client, pdfBase64 string) (*PolicyExtraction, error) {
    schemaMap := generateJSONSchema(&PolicyExtraction{})

    message, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
        MaxTokens:   4096,
        Model:       anthropic.ModelClaudeSonnet4_6,
        Temperature: param.NewOpt(0.0),
        System: []anthropic.TextBlockParam{
            {Text: `Extract structured data from insurance policy documents.
Include: policy number, effective dates, named insureds, coverage types with limits
and deductibles, endorsements, exclusions, and premium breakdown.
For monetary values, use numeric type (no currency symbols).
For dates, use ISO 8601 format.`},
        },
        OutputConfig: anthropic.OutputConfigParam{
            Format: anthropic.JSONOutputFormatParam{Schema: schemaMap},
        },
        Messages: []anthropic.MessageParam{
            anthropic.NewUserMessage(
                anthropic.ContentBlockParamUnion{
                    OfDocument: &anthropic.DocumentBlockParam{
                        Source: anthropic.DocumentBlockParamSourceUnion{
                            OfBase64: &anthropic.Base64PDFSourceParam{Data: pdfBase64},
                        },
                        CacheControl: anthropic.CacheControlEphemeralParam{}, // cache for follow-ups
                    },
                },
                anthropic.NewTextBlock("Extract all policy data from this document."),
            ),
        },
    })
    if err != nil {
        return nil, err
    }

    var result PolicyExtraction
    text := message.Content[0].AsAny().(anthropic.TextBlock).Text
    if err := json.Unmarshal([]byte(text), &result); err != nil {
        return nil, fmt.Errorf("parse response: %w", err)
    }
    return &result, nil
}
```

### Prompt Engineering Tips for Insurance

1. **Be explicit about output format** — use structured outputs rather than asking for JSON in the prompt.
2. **Provide examples of edge cases** — partial coverage, multi-peril policies, endorsement stacking.
3. **Set temperature to 0.0** for extraction and classification; consistency matters more than creativity.
4. **Cache the system prompt + tool definitions** — they don't change between requests.
5. **Use tool use for multi-step workflows** — e.g., look up policy -> check claims -> calculate premium.
6. **Split large documents** — process sections independently, then aggregate. Stay under 600 pages/request.
7. **Validate extracted data** — always verify critical fields (dates, monetary amounts) against source.
8. **Use batches for bulk processing** — renewal processing, portfolio analysis, compliance audits.

---

## Quick Reference: Helper Functions

```go
// Message construction
anthropic.NewUserMessage(blocks ...ContentBlockParamUnion) MessageParam
anthropic.NewAssistantMessage(blocks ...ContentBlockParamUnion) MessageParam

// Content block construction
anthropic.NewTextBlock(text string) ContentBlockParamUnion
anthropic.NewToolResultBlock(toolUseID, content string, isError bool) ContentBlockParamUnion
anthropic.NewToolUseBlock(id string, input any, name string) ContentBlockParamUnion

// Tool choice helpers
anthropic.ToolChoiceParamOfTool(name string) ToolChoiceUnionParam
anthropic.NewToolChoiceNoneParam() ToolChoiceNoneParam

// Optional values (for param.Opt[T] fields)
param.NewOpt[T](value T) param.Opt[T]

// String helper for optional descriptions
anthropic.String(s string) param.Opt[string]
```
