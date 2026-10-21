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