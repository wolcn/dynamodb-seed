<!-- unsloth-chat-v1:[708,11882,848,13175,869,1036,14031,1152,18224] -->

## User

Using the programming language Go and the AWS SDK for Go v2

Write the code for a cli program that puts entries with random values into a DynamoDB table where:

- the table name is supplied as a parameter; required

- the table attribute definitions are supplied as a parameter; required

- the table key schema is supplied as a parameter; required

- the number of entries to be added supplied as a parameter; optional, default 10

- the AWS region suuplied as a parameter; optional, default "eu-north-1"

if the table doesn't exist, create it as an on-demand DynamoDB table with the billing mode PAY_PER_REQUEST, otherwise just add entries

assume that access to the AWS account is already enabled

## Assistant

<details>
<summary>thinking</summary>

The user wants a Go CLI program using AWS SDK for Go v2 that:

1. Takes parameters:
   - Table name (required)
   - Attribute definitions (required)
   - Key schema (required)
   - Number of entries (optional, default 10)
   - AWS region (optional, default "eu-north-1")

2. Behavior:
   - If table doesn't exist, create it as on-demand (PAY_PER_REQUEST)
   - Add random entries to the table

Let me think about the design:

- Use `flag` package for CLI parameters
- Attribute definitions format: something like `id:S,name:S,score:N` (name:type pairs)
- Key schema format: something like `id:HASH` or `id:HASH,partition:HASH,sort:RANGE` - I need to think about this. DynamoDB key schema has HASH and RANGE keys. Let me use a format like `attrName:KEY_TYPE` where KEY_TYPE is HASH or RANGE.
- For random values, I'll generate random strings for S type, random numbers for N type, random binary for B type.

Let me structure the code:

1. Parse flags
2. Initialize AWS config with region
3. Check if table exists (DescribeTable)
4. If not exists, create it with PAY_PER_REQUEST billing mode
5. Generate and put random entries

For the attribute definitions parameter format, I'll use comma-separated `name:type` pairs where type is S, N, or B.
For the key schema, I'll use comma-separated `name:KEY_TYPE` pairs where KEY_TYPE is HASH or RANGE.

Let me write this out properly with AWS SDK for Go v2.

The AWS SDK for Go v2 packages I'll need:
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/service/dynamodb`
- `github.com/aws/aws-sdk-go-v2/service/dynamodb/types`
- `context`

Let me write the complete program.


</details>

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// AttributeDef represents a single attribute definition parsed from the CLI.
type AttributeDef struct {
	Name string
	Type types.ScalarAttributeType
}

// KeySchemaElem represents a single key schema element parsed from the CLI.
type KeySchemaElem struct {
	AttributeName string
	KeyType       types.KeyType
}

func main() {
	tableName := flag.String("table", "", "DynamoDB table name (required)")
	attrDefsStr := flag.String("attributes", "", "Attribute definitions, comma-separated name:type pairs, e.g. 'id:S,name:S,score:N' (required)")
	keySchemaStr := flag.String("key-schema", "", "Key schema, comma-separated name:KEY_TYPE pairs, e.g. 'id:HASH' or 'pk:HASH,sk:RANGE' (required)")
	numEntries := flag.Int("entries", 10, "Number of entries to add (default 10)")
	region := flag.String("region", "eu-north-1", "AWS region (default eu-north-1)")

	flag.Parse()

	if *tableName == "" {
		fatal("--table is required")
	}
	if *attrDefsStr == "" {
		fatal("--attributes is required")
	}
	if *keySchemaStr == "" {
		fatal("--key-schema is required")
	}
	if *numEntries < 1 {
		fatal("--entries must be >= 1")
	}

	attrDefs, err := parseAttributeDefinitions(*attrDefsStr)
	if err != nil {
		fatal("invalid --attributes: %v", err)
	}

	keySchema, err := parseKeySchema(*keySchemaStr)
	if err != nil {
		fatal("invalid --key-schema: %v", err)
	}

	ctx := context.Background()

	// Load AWS config with the specified region.
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region))
	if err != nil {
		fatal("unable to load AWS SDK config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)

	// Ensure the table exists; create it if not.
	if !tableExists(client, ctx, *tableName) {
		fmt.Printf("Table %q does not exist – creating (PAY_PER_REQUEST)…\n", *tableName)
		if err := createTable(client, ctx, *tableName, attrDefs, keySchema); err != nil {
			fatal("failed to create table: %v", err)
		}
		fmt.Println("Waiting for table to become ACTIVE…")
		waitForActive(client, ctx, *tableName)
		fmt.Println("Table is ready.")
	} else {
		fmt.Printf("Table %q already exists.\n", *tableName)
	}

	// Insert random entries.
	fmt.Printf("Inserting %d random entries into %q…\n", *numEntries, *tableName)
	for i := 0; i < *numEntries; i++ {
		item := generateRandomItem(attrDefs, keySchema, i)
		input := &dynamodb.PutItemInput{
			TableName: awsStr(*tableName),
			Item:      item,
		}
		if _, err := client.PutItem(ctx, input); err != nil {
			fatal("PutItem failed (entry %d): %v", i+1, err)
		}
	}
	fmt.Printf("Done. %d entries written to %q in region %s.\n", *numEntries, *tableName, *region)
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func parseAttributeDefinitions(s string) ([]AttributeDef, error) {
	parts := strings.Split(s, ",")
	seen := make(map[string]bool, len(parts))
	defs := make([]AttributeDef, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected 'name:type', got %q", p)
		}
		name := strings.TrimSpace(kv[0])
		typStr := strings.ToUpper(strings.TrimSpace(kv[1]))

		var st types.ScalarAttributeType
		switch typStr {
		case "S":
			st = types.ScalarAttributeTypeS
		case "N":
			st = types.ScalarAttributeTypeN
		case "B":
			st = types.ScalarAttributeTypeB
		default:
			return nil, fmt.Errorf("unknown attribute type %q (use S, N, or B)", typStr)
		}

		if seen[name] {
			return nil, fmt.Errorf("duplicate attribute name %q", name)
		}
		seen[name] = true
		defs = append(defs, AttributeDef{Name: name, Type: st})
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no attributes parsed")
	}
	return defs, nil
}

func parseKeySchema(s string) ([]KeySchemaElem, error) {
	parts := strings.Split(s, ",")
	elems := make([]KeySchemaElem, 0, len(parts))

	hashSeen := false
	rangeSeen := false

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected 'name:KEY_TYPE', got %q", p)
		}
		name := strings.TrimSpace(kv[0])
		ktStr := strings.ToUpper(strings.TrimSpace(kv[1]))

		var kt types.KeyType
		switch ktStr {
		case "HASH":
			if hashSeen {
				return nil, fmt.Errorf("only one HASH key is allowed")
			}
			hashSeen = true
			kt = types.KeyTypeHash
		case "RANGE":
			if rangeSeen {
				return nil, fmt.Errorf("only one RANGE key is allowed")
			}
			rangeSeen = true
			kt = types.KeyTypeRange
		default:
			return nil, fmt.Errorf("unknown key type %q (use HASH or RANGE)", ktStr)
		}

		elems = append(elems, KeySchemaElem{AttributeName: name, KeyType: kt})
	}

	if !hashSeen {
		return nil, fmt.Errorf("key schema must include a HASH key")
	}
	return elems, nil
}

// ---------------------------------------------------------------------------
// DynamoDB operations
// ---------------------------------------------------------------------------

func tableExists(client *dynamodb.Client, ctx context.Context, name string) bool {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: awsStr(name),
	})
	return err == nil
}

func createTable(client *dynamodb.Client, ctx context.Context, name string, attrDefs []AttributeDef, keySchema []KeySchemaElem) error {
	attrDefInputs := make([]types.AttributeDefinition, 0, len(attrDefs))
	for _, d := range attrDefs {
		attrDefInputs = append(attrDefInputs, types.AttributeDefinition{
			AttributeName: awsStr(d.Name),
			AttributeType: d.Type,
		})
	}

	keySchemaInputs := make([]types.KeySchemaElement, 0, len(keySchema))
	for _, k := range keySchema {
		keySchemaInputs = append(keySchemaInputs, types.KeySchemaElement{
			AttributeName: awsStr(k.AttributeName),
			KeyType:       k.KeyType,
		})
	}

	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   awsStr(name),
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: attrDefInputs,
		KeySchema:            keySchemaInputs,
	})
	return err
}

func waitForActive(client *dynamodb.Client, ctx context.Context, name string) {
	waiter := dynamodb.NewTableExistsWaiter(client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: awsStr(name),
	}, 5*time.Minute); err != nil {
		fatal("table did not become ACTIVE in time: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Random data generation
// ---------------------------------------------------------------------------

var letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func generateRandomItem(attrDefs []AttributeDef, keySchema []KeySchemaElem, idx int) map[string]types.AttributeValue {
	item := make(map[string]types.AttributeValue, len(attrDefs))

	// Build a set of key attribute names for uniqueness.
	keyNames := make(map[string]bool, len(keySchema))
	for _, k := range keySchema {
		keyNames[k.AttributeName] = true
	}

	for _, d := range attrDefs {
		switch d.Type {
		case types.ScalarAttributeTypeS:
			if keyNames[d.Name] {
				// Make key values unique per entry.
				item[d.Name] = &types.AttributeValueMemberS{Value: fmt.Sprintf("key-%d-%s", idx, randomString(8))}
			} else {
				item[d.Name] = &types.AttributeValueMemberS{Value: randomString(12)}
			}
		case types.ScalarAttributeTypeN:
			if keyNames[d.Name] {
				item[d.Name] = &types.AttributeValueMemberN{Value: strconv.Itoa(idx)}
			} else {
				item[d.Name] = &types.AttributeValueMemberN{Value: strconv.Itoa(rand.Intn(10000))}
			}
		case types.ScalarAttributeTypeB:
			blob := make([]byte, 8)
			rand.Read(blob)
			item[d.Name] = &types.AttributeValueMemberB{Value: blob}
		}
	}

	return item
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func awsStr(s string) *string { return &s }

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
```

## How it works

### CLI usage

```bash
go mod init dynamodb-seed && go get github.com/aws/aws-sdk-go-v2/...
go build -o seed .

# Simple: single HASH key, 5 entries
./seed \
  --table Users \
  --attributes "id:S,name:S,age:N" \
  --key-schema "id:HASH" \
  --entries 5

# Composite key (HASH + RANGE), explicit region
./seed \
  --table Orders \
  --attributes "pk:S,sk:S,total:N,region:B" \
  --key-schema "pk:HASH,sk:RANGE" \
  --entries 50 \
  --region us-east-1
```

### Key design decisions

| Aspect | Detail |
|---|---|
| **Attribute format** | Comma-separated `name:type` where type ∈ {`S`, `N`, `B`} |
| **Key schema format** | Comma-separated `name:KEY_TYPE` where KEY_TYPE ∈ {`HASH`, `RANGE`}; exactly one HASH is required, at most one RANGE |
| **Table creation** | If `DescribeTable` returns an error (table not found), a `CreateTable` call is made with `BillingMode = PAY_PER_REQUEST` (on-demand). A `TableExistsWaiter` blocks until the table is `ACTIVE`. |
| **Random values** | `S` → random alphanumeric string; `N` → random integer; `B` → 8 random bytes. Key attributes get a deterministic prefix (`key-<idx>-…`) so each entry is unique and won't collide on re-runs within the same batch. |
| **Region / credentials** | Region comes from `--region` (default `eu-north-1`). Credentials are resolved by the standard AWS SDK v2 chain (env vars, shared config file, IAM role, etc.) — no explicit credential flag is needed. |

### Project layout (single-file)

```
dynamodb-seed/
├── go.mod
└── main.go        ← the code above
```

Run `go mod tidy` after creating the module to pull in the SDK dependencies, then `go build`.

## User

Using the programming language Go and the AWS SDK for Go v2

Write the code for a cli program that puts entries with random values into a DynamoDB table where:

- the table name is supplied as a parameter; required

- the table attribute definitions are supplied as a parameter; required

- the table key schema is supplied as a parameter; required

- the number of entries to be added supplied as a parameter; optional, default 10

- the AWS region supplied as a parameter; optional, default "eu-north-1"

Long parameter names shall be prefixed with the double dash "--" and short single letter versions shall be prefixed with a single dash "-"

if the table doesn't exist, create it as an on-demand DynamoDB table with the billing mode PAY_PER_REQUEST, otherwise just add entries

assume that access to the AWS account is already enabled

## Assistant

<details>
<summary>thinking</summary>

The user wants a Go CLI program using AWS SDK for Go v2 that seeds random entries into a DynamoDB table. Let me re-read the requirements carefully:

1. Table name - required parameter
2. Attribute definitions - required parameter
3. Key schema - required parameter
4. Number of entries - optional, default 10
5. AWS region - optional, default "eu-north-1"

Key constraint: Long parameter names prefixed with `--` and short single-letter versions prefixed with `-`.

In Go's standard `flag` package, you can only use single-dash by default (e.g., `-table`). To support both `-t` and `--table`, I need to either:
- Use a third-party library like `pflag` (spf13/pflag) which supports both short and long flags
- Or manually parse the arguments

Let me use `github.com/spf13/pflag` since it's the standard way to do this in Go and supports both `-t` and `--table` syntax natively.

Actually, let me reconsider. The user said "Long parameter names shall be prefixed with the double dash '--' and short single letter versions shall be prefixed with a single dash '-'". This is exactly what pflag does. Let me use it.

Let me design the flags:
- `--table` / `-t` : table name (required)
- `--attributes` / `-a` : attribute definitions (required)
- `--key-schema` / `-k` : key schema (required)
- `--entries` / `-n` : number of entries (optional, default 10)
- `--region` / `-r` : AWS region (optional, default "eu-north-1")

Now for the attribute definitions format, I'll use comma-separated `name:type` pairs where type is S, N, or B.
For key schema, comma-separated `name:KEY_TYPE` where KEY_TYPE is HASH or RANGE.

Let me write clean, well-structured code with proper error handling.

I'll structure it as:
1. Flag parsing with pflag
2. Parse attribute definitions
3. Parse key schema
4. Initialize AWS config
5. Check/create table
6. Generate and put random items

Let me write this out properly.


</details>

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// Types for parsed CLI input
// ---------------------------------------------------------------------------

type AttributeDef struct {
	Name string
	Type types.ScalarAttributeType
}

type KeySchemaElem struct {
	AttributeName string
	KeyType       types.KeyType
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	fs := pflag.NewFlagSet("dynamodb-seed", pflag.ExitOnError)

	tableName := fs.StringP("table", "t", "", "DynamoDB table name (required)")
	attrDefsStr := fs.StringP("attributes", "a", "", `Attribute definitions as comma-separated name:type pairs, e.g. "id:S,name:S,score:N" (required)`)
	keySchemaStr := fs.StringP("key-schema", "k", "", `Key schema as comma-separated name:KEY_TYPE pairs, e.g. "id:HASH" or "pk:HASH,sk:RANGE" (required)`)
	numEntries := fs.IntP("entries", "n", 10, "Number of entries to add (default 10)")
	region := fs.StringP("region", "r", "eu-north-1", "AWS region (default eu-north-1)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\nOptions:\n", os.Args[0])
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		fatal("%v", err)
	}

	// --- validate required flags -------------------------------------------
	if *tableName == "" {
		fatal("--table / -t is required")
	}
	if *attrDefsStr == "" {
		fatal("--attributes / -a is required")
	}
	if *keySchemaStr == "" {
		fatal("--key-schema / -k is required")
	}
	if *numEntries < 1 {
		fatal("--entries / -n must be >= 1")
	}

	// --- parse structured parameters ---------------------------------------
	attrDefs, err := parseAttributeDefinitions(*attrDefsStr)
	if err != nil {
		fatal("invalid --attributes: %v", err)
	}

	keySchema, err := parseKeySchema(*keySchemaStr)
	if err != nil {
		fatal("invalid --key-schema: %v", err)
	}

	ctx := context.Background()

	// --- AWS setup ----------------------------------------------------------
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region))
	if err != nil {
		fatal("unable to load AWS SDK config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)

	// --- ensure table exists ------------------------------------------------
	if !tableExists(client, ctx, *tableName) {
		fmt.Printf("Table %q not found – creating with PAY_PER_REQUEST billing…\n", *tableName)
		if err := createTable(client, ctx, *tableName, attrDefs, keySchema); err != nil {
			fatal("CreateTable failed: %v", err)
		}
		fmt.Println("Waiting for table to become ACTIVE…")
		waitForActive(client, ctx, *tableName)
		fmt.Println("Table is ready.")
	} else {
		fmt.Printf("Table %q already exists.\n", *tableName)
	}

	// --- insert random entries ----------------------------------------------
	fmt.Printf("Inserting %d random entries into %q (region=%s)…\n", *numEntries, *tableName, *region)

	for i := 0; i < *numEntries; i++ {
		item := generateRandomItem(attrDefs, keySchema, i)

		if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: awsStr(*tableName),
			Item:      item,
		}); err != nil {
			fatal("PutItem #%d failed: %v", i+1, err)
		}
	}

	fmt.Printf("Done – %d entries written to %q.\n", *numEntries, *tableName)
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func parseAttributeDefinitions(s string) ([]AttributeDef, error) {
	parts := strings.Split(s, ",")
	seen := make(map[string]bool, len(parts))
	defs := make([]AttributeDef, 0, len(parts))

	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		kv := strings.SplitN(raw, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected 'name:type', got %q", raw)
		}

		name := strings.TrimSpace(kv[0])
		typStr := strings.ToUpper(strings.TrimSpace(kv[1]))

		var st types.ScalarAttributeType
		switch typStr {
		case "S":
			st = types.ScalarAttributeTypeS
		case "N":
			st = types.ScalarAttributeTypeN
		case "B":
			st = types.ScalarAttributeTypeB
		default:
			return nil, fmt.Errorf("unknown type %q (valid: S, N, B)", typStr)
		}

		if seen[name] {
			return nil, fmt.Errorf("duplicate attribute %q", name)
		}
		seen[name] = true
		defs = append(defs, AttributeDef{Name: name, Type: st})
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no valid attributes parsed")
	}
	return defs, nil
}

func parseKeySchema(s string) ([]KeySchemaElem, error) {
	parts := strings.Split(s, ",")
	elems := make([]KeySchemaElem, 0, len(parts))

	hashSeen, rangeSeen := false, false

	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		kv := strings.SplitN(raw, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected 'name:KEY_TYPE', got %q", raw)
		}

		name := strings.TrimSpace(kv[0])
		ktStr := strings.ToUpper(strings.TrimSpace(kv[1]))

		var kt types.KeyType
		switch ktStr {
		case "HASH":
			if hashSeen {
				return nil, fmt.Errorf("only one HASH key allowed")
			}
			hashSeen = true
			kt = types.KeyTypeHash
		case "RANGE":
			if rangeSeen {
				return nil, fmt.Errorf("only one RANGE key allowed")
			}
			rangeSeen = true
			kt = types.KeyTypeRange
		default:
			return nil, fmt.Errorf("unknown key type %q (valid: HASH, RANGE)", ktStr)
		}

		elems = append(elems, KeySchemaElem{AttributeName: name, KeyType: kt})
	}

	if !hashSeen {
		return nil, fmt.Errorf("key schema must include exactly one HASH key")
	}
	return elems, nil
}

// ---------------------------------------------------------------------------
// DynamoDB operations
// ---------------------------------------------------------------------------

func tableExists(client *dynamodb.Client, ctx context.Context, name string) bool {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: awsStr(name),
	})
	return err == nil
}

func createTable(client *dynamodb.Client, ctx context.Context, name string, attrDefs []AttributeDef, keySchema []KeySchemaElem) error {
	attrInputs := make([]types.AttributeDefinition, 0, len(attrDefs))
	for _, d := range attrDefs {
		attrInputs = append(attrInputs, types.AttributeDefinition{
			AttributeName: awsStr(d.Name),
			AttributeType: d.Type,
		})
	}

	keyInputs := make([]types.KeySchemaElement, 0, len(keySchema))
	for _, k := range keySchema {
		keyInputs = append(keyInputs, types.KeySchemaElement{
			AttributeName: awsStr(k.AttributeName),
			KeyType:       k.KeyType,
		})
	}

	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:            awsStr(name),
		BillingMode:          types.BillingModePayPerRequest,
		AttributeDefinitions: attrInputs,
		KeySchema:            keyInputs,
	})
	return err
}

func waitForActive(client *dynamodb.Client, ctx context.Context, name string) {
	waiter := dynamodb.NewTableExistsWaiter(client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: awsStr(name),
	}, 5*time.Minute); err != nil {
		fatal("table did not become ACTIVE within 5 min: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Random data generation
// ---------------------------------------------------------------------------

const alphaNum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphaNum[rand.Intn(len(alphaNum))]
	}
	return string(b)
}

func generateRandomItem(attrDefs []AttributeDef, keySchema []KeySchemaElem, idx int) map[string]types.AttributeValue {
	keyNames := make(map[string]bool, len(keySchema))
	for _, k := range keySchema {
		keyNames[k.AttributeName] = true
	}

	item := make(map[string]types.AttributeValue, len(attrDefs))

	for _, d := range attrDefs {
		switch d.Type {
		case types.ScalarAttributeTypeS:
			if keyNames[d.Name] {
				item[d.Name] = &types.AttributeValueMemberS{
					Value: fmt.Sprintf("key-%d-%s", idx, randomString(8)),
				}
			} else {
				item[d.Name] = &types.AttributeValueMemberS{
					Value: randomString(12),
				}
			}

		case types.ScalarAttributeTypeN:
			if keyNames[d.Name] {
				item[d.Name] = &types.AttributeValueMemberN{
					Value: strconv.Itoa(idx),
				}
			} else {
				item[d.Name] = &types.AttributeValueMemberN{
					Value: strconv.Itoa(rand.Intn(1_000_000)),
				}
			}

		case types.ScalarAttributeTypeB:
			blob := make([]byte, 8)
			rand.Read(blob)
			item[d.Name] = &types.AttributeValueMemberB{Value: blob}
		}
	}

	return item
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func awsStr(s string) *string { return &s }

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\nERROR: "+format+"\n", args...)
	os.Exit(1)
}
```

## `go.mod` (for reference)

```
module dynamodb-seed

go 1.22

require (
	github.com/aws/aws-sdk-go-v2 v1.30.0
	github.com/aws/aws-sdk-go-v2/config v1.27.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.34.0
	github.com/spf13/pflag v1.0.5
)
```

Run `go mod tidy` to resolve all transitive dependencies.

---

## Usage examples

```bash
# Long-form flags
./seed \
  --table Users \
  --attributes "id:S,username:S,age:N" \
  --key-schema "id:HASH" \
  --entries 25 \
  --region eu-west-1

# Short-form flags
./seed \
  -t Orders \
  -a "pk:S,sk:S,total:N,payload:B" \
  -k "pk:HASH,sk:RANGE" \
  -n 50 \
  -r us-east-1

# Mixed (only specify what you need; defaults apply)
./seed \
  -t Events \
  --attributes "event_id:S,ts:N" \
  -k event_id:HASH
```

---

## How the flags map to parameters

| Long flag | Short flag | Required | Default | Description |
|-----------|-----------|----------|---------|-------------|
| `--table` | `-t` | ✔ | — | DynamoDB table name |
| `--attributes` | `-a` | ✔ | — | Comma-separated `name:type` (S, N, B) |
| `--key-schema` | `-k` | ✔ | — | Comma-separated `name:HASH\|RANGE` |
| `--entries` | `-n` | ✘ | `10` | Number of random items to write |
| `--region` | `-r` | ✘ | `eu-north-1` | AWS region |

The dual-dash / single-dash convention is handled by **spf13/pflag**, which natively supports both `--long` and `-s` forms for the same flag via `StringP`, `IntP`, etc.

---

## Behaviour summary

1. **Parse & validate** – all required flags are checked; attribute types and key types are validated against allowed values.
2. **Check table** – `DescribeTable` is called. If it succeeds the table exists; if not, a `CreateTable` call is issued with `BillingMode = PAY_PER_REQUEST`, followed by a waiter that polls until the table reaches `ACTIVE`.
3. **Write entries** – for each index 0…N-1 a map of `AttributeValue` items is generated (unique key values per entry, random non-key values) and submitted via `PutItem`.

## User

Using the programming language Go and the AWS SDK for Go v2

Write the code for a cli program that puts entries with random values into a DynamoDB table where:

- the table name is supplied as a parameter; required

- the table attribute definitions are supplied as a parameter; required

- the table key schema is supplied as a parameter; required

- the number of entries to be added supplied as a parameter; optional, default 10

- the AWS region supplied as a parameter; optional, default "eu-north-1"

Long parameter names shall be prefixed with the double dash "--" and short single letter versions shall be prefixed with a single dash "-"; use pflag for this.

if the table doesn't exist, create it as an on-demand DynamoDB table with the billing mode PAY_PER_REQUEST, otherwise just add entries

assume that access to the AWS account is already enabled

## User

Using the programming language Go and the AWS SDK for Go v2

Write the code for a cli program that puts entries with random values into a DynamoDB table where:

- the table name is supplied as a parameter; required

- the table attribute definitions are supplied as a parameter; required

- the table key schema is supplied as a parameter; required

- the number of entries to be added supplied as a parameter; optional, default 10

- the AWS region supplied as a parameter; optional, default "eu-north-1"

Long parameter names shall be prefixed with the double dash "--" and short single letter versions shall be prefixed with a single dash "-"; use pflag for this

if the table doesn't exist, create it as an on-demand DynamoDB table with the billing mode PAY_PER_REQUEST, otherwise just add entries

If creating a table, only pass attributes that are included in the key schema to the create table function

When adding entries to the table, use all attributes given

Assume that access to the AWS account is already enabled

## Assistant

<details>
<summary>thinking</summary>

The user wants a Go CLI program using AWS SDK for Go v2 and pflag. Let me carefully re-read all requirements:

1. Table name - required parameter
2. Attribute definitions - required parameter
3. Key schema - required parameter
4. Number of entries - optional, default 10
5. AWS region - optional, default "eu-north-1"

Key constraints:
- Long params with `--`, short single-letter with `-`, use pflag
- If table doesn't exist, create it as on-demand (PAY_PER_REQUEST)
- **When creating a table, only pass attributes that are included in the key schema to the CreateTable function** - This is important! The attribute definitions given by the user may include more attributes than just the key attributes. When calling CreateTable, we should only include the key attributes in the AttributeDefinitions parameter.
- **When adding entries to the table, use all attributes given** - So when generating random items for PutItem, we use ALL the attributes from the --attributes parameter.

This makes sense because:
- DynamoDB's CreateTable API requires AttributeDefinitions to list all attributes referenced in the KeySchema (and optionally others, but the user wants only key ones)
- But when putting items, you can include any attribute that was defined in the table (or even new ones if the table allows them)

Wait, actually in DynamoDB, the AttributeDefinitions in CreateTable must include ALL attributes used in the KeySchema. You CAN also include other attributes there, but it's not required for non-key attributes. The user specifically says "only pass attributes that are included in the key schema to the create table function" - so we filter the attribute definitions to only those whose names appear in the key schema.

Let me structure the code:

1. Parse flags with pflag (StringP, IntP for short+long)
2. Parse attribute definitions (name:type pairs)
3. Parse key schema (name:HASH|RANGE pairs)
4. AWS config + client
5. Check if table exists
6. If not, create it - but only pass key attributes in AttributeDefinitions
7. Generate random items using ALL attributes
8. PutItem for each

Let me write this cleanly.

For the flag definitions:
- `--table` / `-t` : string, required
- `--attributes` / `-a` : string, required  
- `--key-schema` / `-k` : string, required
- `--entries` / `-n` : int, default 10
- `--region` / `-r` : string, default "eu-north-1"

Now let me write the complete code.


</details>

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// Parsed CLI types
// ---------------------------------------------------------------------------

type AttributeDef struct {
	Name string
	Type types.ScalarAttributeType
}

type KeySchemaElem struct {
	AttributeName string
	KeyType       types.KeyType
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	fs := pflag.NewFlagSet("dynamodb-seed", pflag.ExitOnError)

	tableName := fs.StringP("table", "t", "", "DynamoDB table name (required)")
	attrStr   := fs.StringP("attributes", "a", "", `Attribute definitions as comma-separated name:type pairs, e.g. "id:S,name:S,score:N" (required)`)
	keyStr    := fs.StringP("key-schema", "k", "", `Key schema as comma-separated name:KEY_TYPE pairs, e.g. "id:HASH" or "pk:HASH,sk:RANGE" (required)`)
	numEntries := fs.IntP("entries", "n", 10, "Number of entries to add (default 10)")
	region    := fs.StringP("region", "r", "eu-north-1", "AWS region (default eu-north-1)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n\nOptions:\n", os.Args[0])
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		fatal("%v", err)
	}

	// --- validate required flags -------------------------------------------
	if *tableName == "" {
		fatal("--table / -t is required")
	}
	if *attrStr == "" {
		fatal("--attributes / -a is required")
	}
	if *keyStr == "" {
		fatal("--key-schema / -k is required")
	}
	if *numEntries < 1 {
		fatal("--entries / -n must be >= 1")
	}

	// --- parse structured parameters ---------------------------------------
	allAttrs, err := parseAttributeDefinitions(*attrStr)
	if err != nil {
		fatal("invalid --attributes: %v", err)
	}

	keySchema, err := parseKeySchema(*keyStr)
	if err != nil {
		fatal("invalid --key-schema: %v", err)
	}

	ctx := context.Background()

	// --- AWS setup ----------------------------------------------------------
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region))
	if err != nil {
		fatal("unable to load AWS SDK config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)

	// --- ensure table exists ------------------------------------------------
	if !tableExists(client, ctx, *tableName) {
		fmt.Printf("Table %q not found – creating (PAY_PER_REQUEST)…\n", *tableName)

		// Only key attributes are passed to CreateTable's AttributeDefinitions.
		keyAttrs := filterKeyAttributes(allAttrs, keySchema)

		if err := createTable(client, ctx, *tableName, keyAttrs, keySchema); err != nil {
			fatal("CreateTable failed: %v", err)
		}

		fmt.Println("Waiting for table to become ACTIVE…")
		waitForActive(client, ctx, *tableName)
		fmt.Println("Table is ready.")
	} else {
		fmt.Printf("Table %q already exists.\n", *tableName)
	}

	// --- insert random entries (using ALL attributes) -----------------------
	fmt.Printf("Inserting %d random entries into %q (region=%s)…\n", *numEntries, *tableName, *region)

	for i := 0; i < *numEntries; i++ {
		item := generateRandomItem(allAttrs, keySchema, i)

		if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: awsStr(*tableName),
			Item:      item,
		}); err != nil {
			fatal("PutItem #%d failed: %v", i+1, err)
		}
	}

	fmt.Printf("Done – %d entries written to %q.\n", *numEntries, *tableName)
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func parseAttributeDefinitions(s string) ([]AttributeDef, error) {
	parts := strings.Split(s, ",")
	seen := make(map[string]bool, len(parts))
	defs := make([]AttributeDef, 0, len(parts))

	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		kv := strings.SplitN(raw, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected 'name:type', got %q", raw)
		}

		name := strings.TrimSpace(kv[0])
		typStr := strings.ToUpper(strings.TrimSpace(kv[1]))

		var st types.ScalarAttributeType
		switch typStr {
		case "S":
			st = types.ScalarAttributeTypeS
		case "N":
			st = types.ScalarAttributeTypeN
		case "B":
			st = types.ScalarAttributeTypeB
		default:
			return nil, fmt.Errorf("unknown type %q (valid: S, N, B)", typStr)
		}

		if seen[name] {
			return nil, fmt.Errorf("duplicate attribute %q", name)
		}
		seen[name] = true
		defs = append(defs, AttributeDef{Name: name, Type: st})
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no valid attributes parsed")
	}
	return defs, nil
}

func parseKeySchema(s string) ([]KeySchemaElem, error) {
	parts := strings.Split(s, ",")
	elems := make([]KeySchemaElem, 0, len(parts))

	hashSeen, rangeSeen := false, false

	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		kv := strings.SplitN(raw, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected 'name:KEY_TYPE', got %q", raw)
		}

		name := strings.TrimSpace(kv[0])
		ktStr := strings.ToUpper(strings.TrimSpace(kv[1]))

		var kt types.KeyType
		switch ktStr {
		case "HASH":
			if hashSeen {
				return nil, fmt.Errorf("only one HASH key allowed")
			}
			hashSeen = true
			kt = types.KeyTypeHash
		case "RANGE":
			if rangeSeen {
				return nil, fmt.Errorf("only one RANGE key allowed")
			}
			rangeSeen = true
			kt = types.KeyTypeRange
		default:
			return nil, fmt.Errorf("unknown key type %q (valid: HASH, RANGE)", ktStr)
		}

		elems = append(elems, KeySchemaElem{AttributeName: name, KeyType: kt})
	}

	if !hashSeen {
		return nil, fmt.Errorf("key schema must include exactly one HASH key")
	}
	return elems, nil
}

// filterKeyAttributes returns only those attribute definitions whose names
// appear in the key schema. This is what gets passed to CreateTable.
func filterKeyAttributes(all []AttributeDef, keySchema []KeySchemaElem) []AttributeDef {
	keyNames := make(map[string]bool, len(keySchema))
	for _, k := range keySchema {
		keyNames[k.AttributeName] = true
	}

	filtered := make([]AttributeDef, 0, len(keySchema))
	for _, a := range all {
		if keyNames[a.Name] {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

// ---------------------------------------------------------------------------
// DynamoDB operations
// ---------------------------------------------------------------------------

func tableExists(client *dynamodb.Client, ctx context.Context, name string) bool {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: awsStr(name),
	})
	return err == nil
}

func createTable(client *dynamodb.Client, ctx context.Context, name string, keyAttrs []AttributeDef, keySchema []KeySchemaElem) error {
	attrInputs := make([]types.AttributeDefinition, 0, len(keyAttrs))
	for _, a := range keyAttrs {
		attrInputs = append(attrInputs, types.AttributeDefinition{
			AttributeName: awsStr(a.Name),
			AttributeType: a.Type,
		})
	}

	keyInputs := make([]types.KeySchemaElement, 0, len(keySchema))
	for _, k := range keySchema {
		keyInputs = append(keyInputs, types.KeySchemaElement{
			AttributeName: awsStr(k.AttributeName),
			KeyType:       k.KeyType,
		})
	}

	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:            awsStr(name),
		BillingMode:          types.BillingModePayPerRequest,
		AttributeDefinitions: attrInputs,
		KeySchema:            keyInputs,
	})
	return err
}

func waitForActive(client *dynamodb.Client, ctx context.Context, name string) {
	waiter := dynamodb.NewTableExistsWaiter(client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: awsStr(name),
	}, 5*time.Minute); err != nil {
		fatal("table did not become ACTIVE within 5 min: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Random data generation
// ---------------------------------------------------------------------------

const alphaNum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphaNum[rand.Intn(len(alphaNum))]
	}
	return string(b)
}

// generateRandomItem builds a full item using ALL supplied attributes.
// Key attributes receive unique values per entry index to avoid collisions.
func generateRandomItem(allAttrs []AttributeDef, keySchema []KeySchemaElem, idx int) map[string]types.AttributeValue {
	keyNames := make(map[string]bool, len(keySchema))
	for _, k := range keySchema {
		keyNames[k.AttributeName] = true
	}

	item := make(map[string]types.AttributeValue, len(allAttrs))

	for _, a := range allAttrs {
		switch a.Type {
		case types.ScalarAttributeTypeS:
			if keyNames[a.Name] {
				item[a.Name] = &types.AttributeValueMemberS{
					Value: fmt.Sprintf("key-%d-%s", idx, randomString(8)),
				}
			} else {
				item[a.Name] = &types.AttributeValueMemberS{
					Value: randomString(12),
				}
			}

		case types.ScalarAttributeTypeN:
			if keyNames[a.Name] {
				item[a.Name] = &types.AttributeValueMemberN{
					Value: strconv.Itoa(idx),
				}
			} else {
				item[a.Name] = &types.AttributeValueMemberN{
					Value: strconv.Itoa(rand.Intn(1_000_000)),
				}
			}

		case types.ScalarAttributeTypeB:
			blob := make([]byte, 8)
			rand.Read(blob)
			item[a.Name] = &types.AttributeValueMemberB{Value: blob}
		}
	}

	return item
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func awsStr(s string) *string { return &s }

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\nERROR: "+format+"\n", args...)
	os.Exit(1)
}
```

---

## `go.mod`

```
module dynamodb-seed

go 1.22

require (
	github.com/aws/aws-sdk-go-v2 v1.30.0
	github.com/aws/aws-sdk-go-v2/config v1.27.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.34.0
	github.com/spf13/pflag v1.0.5
)
```

Run `go mod tidy` to resolve transitive dependencies, then `go build -o seed .`.

---

## Usage

```bash
# Long-form
./seed \
  --table Users \
  --attributes "id:S,username:S,age:N,email:S" \
  --key-schema "id:HASH" \
  --entries 25 \
  --region eu-west-1

# Short-form
./seed \
  -t Orders \
  -a "pk:S,sk:S,total:N,status:S,payload:B" \
  -k "pk:HASH,sk:RANGE" \
  -n 50 \
  -r us-east-1
```

---

## Flag reference

| Long | Short | Required | Default | Purpose |
|------|-------|----------|---------|---------|
| `--table` | `-t` | ✔ | — | DynamoDB table name |
| `--attributes` | `-a` | ✔ | — | All attributes: `name:S\|N\|B,…` |
| `--key-schema` | `-k` | ✔ | — | Key definition: `name:HASH[,name:RANGE]` |
| `--entries` | `-n` | ✘ | `10` | How many random items to write |
| `--region` | `-r` | ✘ | `eu-north-1` | AWS region |

---

## Key design point: attribute filtering on table creation

```
--attributes "id:S,username:S,age:N,email:S"
--key-schema "id:HASH"
```

When **creating** the table, only `id:S` is passed to `CreateTable.AttributeDefinitions` because it is the only attribute named in the key schema. The other attributes (`username`, `age`, `email`) are omitted from that API call.

When **putting items**, all five attributes are populated with random values and sent via `PutItem`.

## User

Using the programming language Go and the AWS SDK for Go v2

Write the code for a cli program that puts entries with random values into a DynamoDB table where:

- the table name is supplied as a parameter; required

- the table attribute definitions are supplied as a parameter; required

- the table key schema is supplied as a parameter; required

- the number of entries to be added supplied as a parameter; optional, default 10

- the AWS region supplied as a parameter; optional, default "eu-north-1"

Long parameter names shall be prefixed with the double dash "--" and short single letter versions shall be prefixed with a single dash "-"; use pflag for this

If no parameters are given or if "--help"/"-h" is given, print a summary of the parameter list with example values

if the table doesn't exist, create it as an on-demand DynamoDB table with the billing mode PAY_PER_REQUEST, otherwise just add entries

If creating a table, only pass attributes that are included in the key schema to the create table function

When adding entries to the table, use all attributes given

Assume that access to the AWS account is already enabled

## Assistant

<details>
<summary>thinking</summary>

The user wants a Go CLI program using AWS SDK for Go v2 and pflag. Let me carefully go through all requirements:

1. Table name - required parameter
2. Attribute definitions - required parameter
3. Key schema - required parameter
4. Number of entries - optional, default 10
5. AWS region - optional, default "eu-north-1"

Constraints:
- Long params with `--`, short single-letter with `-`, use pflag
- If no parameters given OR `--help`/`-h` is given, print a summary of the parameter list with example values
- If table doesn't exist, create it as on-demand (PAY_PER_REQUEST)
- When creating table, only pass attributes that are in the key schema to CreateTable
- When adding entries, use ALL attributes given
- Assume AWS access is already enabled

For the help behavior: pflag has built-in `--help`/`-h` handling. But the user wants "if no parameters are given" to also show help. I need to handle that case manually - check if `fs.NArg() == 0` and no flags were set. Actually, with pflag, if no arguments are passed at all, `os.Args[1:]` will be empty. I can check `len(os.Args) <= 1` before parsing.

Wait, but pflag's `-h`/`--help` is automatically added. When triggered, it prints usage and exits with code 0 (or 2 depending on config). The user wants a custom summary with example values. So I should:
1. Check if no args at all → print help
2. Override the help/usage function to show examples

Actually, let me think about this more carefully. pflag automatically adds `-h` and `--help` flags. When they're triggered, it calls `fs.Usage()` and exits. I can override `fs.Usage` to print my custom summary with examples. And for the "no parameters" case, I check before parsing if `len(os.Args) == 1`.

Let me structure:
- Before parsing: if `len(os.Args) <= 1`, print help and exit
- Override `fs.Usage` to show a nice summary with examples (this handles `-h`/`--help`)

Actually, I should be careful. If I set `fs.Usage`, then when pflag encounters `--help` or `-h`, it will call my custom usage function. That's perfect.

For the "no parameters" case: I check `len(os.Args) <= 1` before calling `fs.Parse`. If true, print help and exit.

Let me write the code now. I'll make the help output informative with example values for each parameter.

Let me also think about the pflag configuration. By default pflag uses `pflag.ExitOnError` which means it will call `os.Exit(2)` on parse errors. For help, it calls `Usage()` and exits with code 0 (actually I think it's `os.Exit(1)` by default for ExitOnError... let me check).

Actually with `pflag.ExitOnError`, when `-h` is triggered:
- It prints usage (calls `fs.Usage()`)
- Exits with code 0

Wait, no. Let me look at this more carefully. In pflag, the help flag is defined as a bool flag. When it's set to true, after parsing, pflag checks if help was requested and calls `f.usage()` then exits. The exit code depends on the error handling strategy.

Actually, I think the cleanest approach is:
1. Use `pflag.ContinueOnError` so I can handle errors myself
2. Check for help manually OR override usage
3. Handle the "no args" case

Let me use `pflag.ExitOnError` but override `fs.Usage` to print my custom help. And separately check for no-args before parsing.

Wait, actually with ExitOnError, when -h is given, pflag will:
1. Parse successfully (help flag is set to true)
2. Call f.usage() 
3. os.Exit(0)

So if I override `fs.Usage`, it should work. Let me verify: in pflag source, the help flag handling does:
```go
if f.helpFlag != nil {
    // ...
    f.usage()
    os.Exit(0)
}
```

And `f.usage()` calls `f.Usage` if it's set. So overriding `fs.Usage` should work for the `-h`/`--help` case.

For the no-args case, I'll check before parsing.

Let me write clean, complete code now.


</details>

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// Parsed CLI types
// ---------------------------------------------------------------------------

type AttributeDef struct {
	Name string
	Type types.ScalarAttributeType
}

type KeySchemaElem struct {
	AttributeName string
	KeyType       types.KeyType
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	// If no arguments at all, show help and exit.
	if len(os.Args) <= 1 {
		printHelp()
		os.Exit(0)
	}

	fs := pflag.NewFlagSet("dynamodb-seed", pflag.ExitOnError)

	tableName  := fs.StringP("table", "t", "", "DynamoDB table name")
	attrStr    := fs.StringP("attributes", "a", "", `Attribute definitions (comma-separated name:type pairs)`)
	keyStr     := fs.StringP("key-schema", "k", "", `Key schema (comma-separated name:KEY_TYPE pairs)`)
	numEntries := fs.IntP("entries", "n", 10, "Number of entries to add")
	region     := fs.StringP("region", "r", "eu-north-1", "AWS region")

	// Override usage so that --help / -h prints our custom summary.
	fs.Usage = func() { printHelp() }

	if err := fs.Parse(os.Args[1:]); err != nil {
		fatal("%v", err)
	}

	// --- validate required flags -------------------------------------------
	if *tableName == "" {
		fatal("--table / -t is required")
	}
	if *attrStr == "" {
		fatal("--attributes / -a is required")
	}
	if *keyStr == "" {
		fatal("--key-schema / -k is required")
	}
	if *numEntries < 1 {
		fatal("--entries / -n must be >= 1")
	}

	// --- parse structured parameters ---------------------------------------
	allAttrs, err := parseAttributeDefinitions(*attrStr)
	if err != nil {
		fatal("invalid --attributes: %v", err)
	}

	keySchema, err := parseKeySchema(*keyStr)
	if err != nil {
		fatal("invalid --key-schema: %v", err)
	}

	ctx := context.Background()

	// --- AWS setup ----------------------------------------------------------
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region))
	if err != nil {
		fatal("unable to load AWS SDK config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)

	// --- ensure table exists ------------------------------------------------
	if !tableExists(client, ctx, *tableName) {
		fmt.Printf("Table %q not found – creating (PAY_PER_REQUEST)…\n", *tableName)

		// Only attributes referenced in the key schema are passed to CreateTable.
		keyAttrs := filterKeyAttributes(allAttrs, keySchema)

		if err := createTable(client, ctx, *tableName, keyAttrs, keySchema); err != nil {
			fatal("CreateTable failed: %v", err)
		}

		fmt.Println("Waiting for table to become ACTIVE…")
		waitForActive(client, ctx, *tableName)
		fmt.Println("Table is ready.")
	} else {
		fmt.Printf("Table %q already exists.\n", *tableName)
	}

	// --- insert random entries (using ALL attributes) -----------------------
	fmt.Printf("Inserting %d random entries into %q (region=%s)…\n", *numEntries, *tableName, *region)

	for i := 0; i < *numEntries; i++ {
		item := generateRandomItem(allAttrs, keySchema, i)

		if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: awsStr(*tableName),
			Item:      item,
		}); err != nil {
			fatal("PutItem #%d failed: %v", i+1, err)
		}
	}

	fmt.Printf("Done – %d entries written to %q.\n", *numEntries, *tableName)
}

// ---------------------------------------------------------------------------
// Help output
// ---------------------------------------------------------------------------

func printHelp() {
	fmt.Println(`dynamodb-seed – insert random entries into a DynamoDB table

Usage:
  dynamodb-seed [OPTIONS]

Options:
  -t, --table string          DynamoDB table name (required)
      Example: "Users"

  -a, --attributes string     Attribute definitions as comma-separated name:type pairs (required)
                              Valid types: S (string), N (number), B (binary)
      Example: "id:S,username:S,age:N,payload:B"

  -k, --key-schema string     Key schema as comma-separated name:KEY_TYPE pairs (required)
                              Valid key types: HASH, RANGE (exactly one HASH, optional one RANGE)
      Example: "id:HASH"  or  "pk:HASH,sk:RANGE"

  -n, --entries int           Number of random entries to add (default 10)
      Example: 25

  -r, --region string         AWS region (default "eu-north-1")
      Example: "us-east-1"

  -h, --help                  Show this help message

Examples:
  # Single HASH key, 5 entries, default region
  dynamodb-seed -t Users -a "id:S,name:S,age:N" -k "id:HASH" -n 5

  # Composite key (HASH + RANGE), explicit region
  dynamodb-seed \
    --table Orders \
    --attributes "pk:S,sk:S,total:N,status:S" \
    --key-schema "pk:HASH,sk:RANGE" \
    --entries 50 \
    --region eu-west-1

Behaviour:
  • If the table does not exist it is created with on-demand billing (PAY_PER_REQUEST).
    Only attributes referenced in the key schema are declared at creation time.
  • Random items are then written using ALL supplied attributes.`)
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func parseAttributeDefinitions(s string) ([]AttributeDef, error) {
	parts := strings.Split(s, ",")
	seen := make(map[string]bool, len(parts))
	defs := make([]AttributeDef, 0, len(parts))

	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		kv := strings.SplitN(raw, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected 'name:type', got %q", raw)
		}

		name := strings.TrimSpace(kv[0])
		typStr := strings.ToUpper(strings.TrimSpace(kv[1]))

		var st types.ScalarAttributeType
		switch typStr {
		case "S":
			st = types.ScalarAttributeTypeS
		case "N":
			st = types.ScalarAttributeTypeN
		case "B":
			st = types.ScalarAttributeTypeB
		default:
			return nil, fmt.Errorf("unknown type %q (valid: S, N, B)", typStr)
		}

		if seen[name] {
			return nil, fmt.Errorf("duplicate attribute %q", name)
		}
		seen[name] = true
		defs = append(defs, AttributeDef{Name: name, Type: st})
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no valid attributes parsed")
	}
	return defs, nil
}

func parseKeySchema(s string) ([]KeySchemaElem, error) {
	parts := strings.Split(s, ",")
	elems := make([]KeySchemaElem, 0, len(parts))

	hashSeen, rangeSeen := false, false

	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		kv := strings.SplitN(raw, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected 'name:KEY_TYPE', got %q", raw)
		}

		name := strings.TrimSpace(kv[0])
		ktStr := strings.ToUpper(strings.TrimSpace(kv[1]))

		var kt types.KeyType
		switch ktStr {
		case "HASH":
			if hashSeen {
				return nil, fmt.Errorf("only one HASH key allowed")
			}
			hashSeen = true
			kt = types.KeyTypeHash
		case "RANGE":
			if rangeSeen {
				return nil, fmt.Errorf("only one RANGE key allowed")
			}
			rangeSeen = true
			kt = types.KeyTypeRange
		default:
			return nil, fmt.Errorf("unknown key type %q (valid: HASH, RANGE)", ktStr)
		}

		elems = append(elems, KeySchemaElem{AttributeName: name, KeyType: kt})
	}

	if !hashSeen {
		return nil, fmt.Errorf("key schema must include exactly one HASH key")
	}
	return elems, nil
}

// filterKeyAttributes returns only those attribute definitions whose names
// appear in the key schema – used for the CreateTable call.
func filterKeyAttributes(all []AttributeDef, keySchema []KeySchemaElem) []AttributeDef {
	keyNames := make(map[string]bool, len(keySchema))
	for _, k := range keySchema {
		keyNames[k.AttributeName] = true
	}

	filtered := make([]AttributeDef, 0, len(keySchema))
	for _, a := range all {
		if keyNames[a.Name] {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

// ---------------------------------------------------------------------------
// DynamoDB operations
// ---------------------------------------------------------------------------

func tableExists(client *dynamodb.Client, ctx context.Context, name string) bool {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: awsStr(name),
	})
	return err == nil
}

func createTable(client *dynamodb.Client, ctx context.Context, name string, keyAttrs []AttributeDef, keySchema []KeySchemaElem) error {
	attrInputs := make([]types.AttributeDefinition, 0, len(keyAttrs))
	for _, a := range keyAttrs {
		attrInputs = append(attrInputs, types.AttributeDefinition{
			AttributeName: awsStr(a.Name),
			AttributeType: a.Type,
		})
	}

	keyInputs := make([]types.KeySchemaElement, 0, len(keySchema))
	for _, k := range keySchema {
		keyInputs = append(keyInputs, types.KeySchemaElement{
			AttributeName: awsStr(k.AttributeName),
			KeyType:       k.KeyType,
		})
	}

	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:            awsStr(name),
		BillingMode:          types.BillingModePayPerRequest,
		AttributeDefinitions: attrInputs,
		KeySchema:            keyInputs,
	})
	return err
}

func waitForActive(client *dynamodb.Client, ctx context.Context, name string) {
	waiter := dynamodb.NewTableExistsWaiter(client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: awsStr(name),
	}, 5*time.Minute); err != nil {
		fatal("table did not become ACTIVE within 5 min: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Random data generation
// ---------------------------------------------------------------------------

const alphaNum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphaNum[rand.Intn(len(alphaNum))]
	}
	return string(b)
}

// generateRandomItem builds an item using ALL supplied attributes.
// Key attributes get unique per-index values to avoid collisions.
func generateRandomItem(allAttrs []AttributeDef, keySchema []KeySchemaElem, idx int) map[string]types.AttributeValue {
	keyNames := make(map[string]bool, len(keySchema))
	for _, k := range keySchema {
		keyNames[k.AttributeName] = true
	}

	item := make(map[string]types.AttributeValue, len(allAttrs))

	for _, a := range allAttrs {
		switch a.Type {
		case types.ScalarAttributeTypeS:
			if keyNames[a.Name] {
				item[a.Name] = &types.AttributeValueMemberS{
					Value: fmt.Sprintf("key-%d-%s", idx, randomString(8)),
				}
			} else {
				item[a.Name] = &types.AttributeValueMemberS{
					Value: randomString(12),
				}
			}

		case types.ScalarAttributeTypeN:
			if keyNames[a.Name] {
				item[a.Name] = &types.AttributeValueMemberN{
					Value: strconv.Itoa(idx),
				}
			} else {
				item[a.Name] = &types.AttributeValueMemberN{
					Value: strconv.Itoa(rand.Intn(1_000_000)),
				}
			}

		case types.ScalarAttributeTypeB:
			blob := make([]byte, 8)
			rand.Read(blob)
			item[a.Name] = &types.AttributeValueMemberB{Value: blob}
		}
	}

	return item
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func awsStr(s string) *string { return &s }

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\nERROR: "+format+"\n", args...)
	os.Exit(1)
}
```

---

## `go.mod`

```
module dynamodb-seed

go 1.22

require (
	github.com/aws/aws-sdk-go-v2 v1.30.0
	github.com/aws/aws-sdk-go-v2/config v1.27.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.34.0
	github.com/spf13/pflag v1.0.5
)
```

Run `go mod tidy` then `go build -o seed .`.

---

## Help output (shown with no args or `-h`/`--help`)

```
dynamodb-seed – insert random entries into a DynamoDB table

Usage:
  dynamodb-seed [OPTIONS]

Options:
  -t, --table string          DynamoDB table name (required)
      Example: "Users"

  -a, --attributes string     Attribute definitions as comma-separated name:type pairs (required)
                              Valid types: S (string), N (number), B (binary)
      Example: "id:S,username:S,age:N,payload:B"

  -k, --key-schema string     Key schema as comma-separated name:KEY_TYPE pairs (required)
                              Valid key types: HASH, RANGE (exactly one HASH, optional one RANGE)
      Example: "id:HASH"  or  "pk:HASH,sk:RANGE"

  -n, --entries int           Number of random entries to add (default 10)
      Example: 25

  -r, --region string         AWS region (default "eu-north-1")
      Example: "us-east-1"

  -h, --help                  Show this help message

Examples:
  # Single HASH key, 5 entries, default region
  dynamodb-seed -t Users -a "id:S,name:S,age:N" -k "id:HASH" -n 5

  # Composite key (HASH + RANGE), explicit region
  dynamodb-seed \
    --table Orders \
    --attributes "pk:S,sk:S,total:N,status:S" \
    --key-schema "pk:HASH,sk:RANGE" \
    --entries 50 \
    --region eu-west-1

Behaviour:
  • If the table does not exist it is created with on-demand billing (PAY_PER_REQUEST).
    Only attributes referenced in the key schema are declared at creation time.
  • Random items are then written using ALL supplied attributes.
```

---

## How the help trigger works

| Situation | What happens |
|-----------|-------------|
| `./seed` (no args) | `len(os.Args) <= 1` check → `printHelp()` + exit 0 |
| `./seed --help` or `./seed -h` | pflag sets its built-in help flag → calls overridden `fs.Usage` → `printHelp()` + exit 0 |
| `./seed -t Users …` (valid) | Normal execution |
| Missing required flag | Error message + exit 1 |

---

## Attribute filtering on table creation

Given:

```
--attributes "id:S, username:S, age:N, payload:B"
--key-schema   "id:HASH"
```

| Phase | Attributes used |
|-------|----------------|
| `CreateTable` → `AttributeDefinitions` | **Only** `id:S` (the key attribute) |
| `PutItem` → `Item` map | **All four**: `id`, `username`, `age`, `payload` |

This is enforced by the `filterKeyAttributes` helper, which intersects the full attribute list with the set of names appearing in the key schema before building the `CreateTableInput`.
