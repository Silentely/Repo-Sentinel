package digest

import (
	"sort"
	"strings"
)

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "update": true,
	"bump": true, "from": true, "into": true, "release": true, "action": true,
	"dependency": true, "dependencies": true, "version": true, "fix": true,
	"feat": true, "chore": true, "refactor": true, "test": true,
}

// ExtractTopEventKeywords 从事件标题集合中提取出现频次最高的核心技术关键词（最多返回 topN 个）。
func ExtractTopEventKeywords(titles []string, topN int) []string {
	if len(titles) == 0 {
		return nil
	}
	if topN <= 0 {
		topN = 5
	}

	counts := make(map[string]int)
	for _, title := range titles {
		words := strings.Fields(title)
		for _, w := range words {
			clean := strings.ToLower(strings.Trim(w, "[](){}:,#!@\"'"))
			if len(clean) >= 3 && !stopWords[clean] {
				counts[clean]++
			}
		}
	}

	type kw struct {
		word  string
		count int
	}
	var list []kw
	for k, v := range counts {
		list = append(list, kw{k, v})
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].word < list[j].word
	})

	var res []string
	for i, item := range list {
		if i >= topN {
			break
		}
		res = append(res, item.word)
	}

	return res
}
