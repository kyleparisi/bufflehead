#!/usr/bin/env python3
"""Generate sample.parquet with nested/complex types."""
import pyarrow as pa
import pyarrow.parquet as pq
import random
import datetime

random.seed(42)
n = 500

roles = ["admin", "user", "editor", "viewer"]
tags_pool = ["python", "go", "rust", "js", "sql", "parquet", "arrow", "duckdb", "react", "vue"]

ids = list(range(1, n + 1))
names = [f"user_{i}" for i in ids]
scores = [random.randint(1, 100) for _ in ids]
user_roles = [random.choice(roles) for _ in ids]
created = [datetime.datetime(2024, 1, 1) + datetime.timedelta(hours=random.randint(0, 8760)) for _ in ids]

# Nested: array of structs (tags with name + weight)
tags = []
for _ in ids:
    num_tags = random.randint(1, 4)
    picked = random.sample(tags_pool, num_tags)
    tags.append([{"name": t, "weight": round(random.uniform(0.1, 1.0), 2)} for t in picked])

# Nested: struct column (address with city + country)
cities = ["New York", "London", "Tokyo", "Berlin", "Sydney", "Toronto", "Paris", "Seoul"]
countries = {"New York": "US", "London": "UK", "Tokyo": "JP", "Berlin": "DE", "Sydney": "AU", "Toronto": "CA", "Paris": "FR", "Seoul": "KR"}
addresses = []
for _ in ids:
    city = random.choice(cities)
    addresses.append({"city": city, "country": countries[city], "zip": f"{random.randint(10000, 99999)}"})

# Nested: simple array of strings (emails)
emails = []
for i, name in enumerate(names):
    domains = ["gmail.com", "company.io", "example.org"]
    num_emails = random.randint(1, 3)
    emails.append([f"{name}@{random.choice(domains)}" for _ in range(num_emails)])

# Build table
tag_type = pa.struct([("name", pa.string()), ("weight", pa.float64())])
address_type = pa.struct([("city", pa.string()), ("country", pa.string()), ("zip", pa.string())])

table = pa.table({
    "id": pa.array(ids, type=pa.int64()),
    "name": pa.array(names, type=pa.string()),
    "score": pa.array(scores, type=pa.int32()),
    "role": pa.array(user_roles, type=pa.string()),
    "created_at": pa.array(created, type=pa.timestamp("us")),
    "tags": pa.array(tags, type=pa.list_(tag_type)),
    "address": pa.array(addresses, type=address_type),
    "emails": pa.array(emails, type=pa.list_(pa.string())),
})

pq.write_table(table, "sample.parquet", compression="snappy")
print(f"Wrote {len(table)} rows, {len(table.schema)} columns to sample.parquet")
print("Schema:")
print(table.schema)
