## dynamodb-seed

Simple utility to seed DynamoDB tables with random data

Created using [Unsloth/Qwen3.8-27B-GGUF](https://unsloth.ai/docs/models/qwen3.8) on an Asus Zenbook 14 with an AMD Ryzen AI 7 350 chipset and 32GB RAM running Ubuntu 26.04.1 with ROCm and llama.cpp

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

Build with the usual:
```
go mod tidy
go build
```
