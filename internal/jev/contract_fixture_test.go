package jev

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestContractFixture(t *testing.T) {
	for _, name := range []string{"systemone.request.json", "systemone.response.json"} {
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(raw, &top); err != nil {
			t.Fatal(err)
		}
		keys := []string{"model", "state", "questions"}
		if name == "systemone.response.json" {
			keys = []string{"model", "answers", "usage"}
		}
		for _, key := range keys {
			if len(top[key]) == 0 || string(top[key]) == "null" {
				t.Fatalf("缺字段 %s", key)
			}
		}
	}
}

func TestEvaluationCasesJSONL(t *testing.T) {
	file, err := os.Open("evaluation/cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("cases.jsonl 行 %d 解析失败: %v", count+1, err)
		}
		for _, req := range []string{"id", "plane", "language", "state", "expected", "reviewed_by"} {
			if len(item[req]) == 0 || string(item[req]) == "null" {
				t.Fatalf("cases.jsonl 行 %d 缺少必需字段 %s", count+1, req)
			}
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("cases.jsonl 不能为空")
	}
}
