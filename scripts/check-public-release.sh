#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
version="${1:-}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo 'usage: check-public-release.sh vX.Y.Z' >&2
  exit 1
fi
scripts/check-module-manifest.sh "$version"
consumer_temp="$(mktemp -d "${TMPDIR:-/tmp}/search-public-consumer.XXXXXX")"
trap 'rm -rf "$consumer_temp"' EXIT
cat > "$consumer_temp/go.mod" <<EOF_MOD
module example.com/search-release-check

go 1.25.0

require (
 github.com/goforj/search $version
 github.com/goforj/search/driver/blevesearch $version
)
EOF_MOD
cat > "$consumer_temp/search_test.go" <<'EOF_GO'
package consumer_test

import (
 "context"
 "testing"
 "github.com/goforj/search"
 "github.com/goforj/search/driver/blevesearch"
 "github.com/goforj/search/driver/meilisearch"
 "github.com/goforj/search/driver/typesensesearch"
)

// TestPublishedComposition verifies independently downloaded root and driver modules work together.
func TestPublishedComposition(t *testing.T) {
 ctx := context.Background()
 driver, err := blevesearch.Open(ctx,blevesearch.Config{Memory:true})
 if err != nil { t.Fatal(err) }
 defer driver.Close()
 definition := search.Definition{Name:"products",Fields:[]search.Field{{Name:"title",Type:search.TextField,Searchable:true}}}
 if err := driver.CreateIndex(ctx,definition);err!=nil{t.Fatal(err)}
 index,err:=search.NewIndex(driver,definition)
 if err!=nil{t.Fatal(err)}
 receipt,err:=search.Upsert(ctx,index,"one",map[string]string{"title":"blue mug"})
 if err!=nil{t.Fatal(err)}
 if outcome,err:=index.Wait(ctx,receipt);err!=nil||outcome.State!=search.OutcomeSucceeded{t.Fatal(outcome,err)}
 if result,err:=index.Search(ctx,search.Query{Text:"blue mug"});err!=nil||result.Total.Value!=1{t.Fatal(result,err)}
 meili,err:=meilisearch.New(meilisearch.Config{URL:"http://127.0.0.1:7700"})
 if err!=nil{t.Fatal(err)}
 defer meili.Close()
 typesense,err:=typesensesearch.New(typesensesearch.Config{URL:"http://127.0.0.1:8108"})
 if err!=nil{t.Fatal(err)}
 defer typesense.Close()
}
EOF_GO
cd "$consumer_temp"
GOWORK=off go mod tidy
GOWORK=off go list -m -json all > modules.json
if rg -q '"Replace"' modules.json; then echo 'published consumer unexpectedly uses a replacement' >&2; exit 1; fi
GOWORK=off go test -race -count=1 ./...
