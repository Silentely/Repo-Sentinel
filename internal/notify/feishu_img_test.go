package notify

import (
	"testing"
)

func TestBuildFeishuImageElement(t *testing.T) {
	elem := BuildFeishuImageElement("img_v2_12345", "架构图", FeishuImageModeCropCenter, true)

	if elem["tag"] != "img" {
		t.Fatalf("expected tag img, got %v", elem["tag"])
	}
	if elem["img_key"] != "img_v2_12345" {
		t.Errorf("expected img_key img_v2_12345, got %v", elem["img_key"])
	}
	if elem["mode"] != "crop_center" {
		t.Errorf("expected mode crop_center, got %v", elem["mode"])
	}
	if elem["preview"] != true {
		t.Errorf("expected preview true, got %v", elem["preview"])
	}

	alt, ok := elem["alt"].(map[string]interface{})
	if !ok || alt["content"] != "架构图" {
		t.Errorf("unexpected alt content: %v", elem["alt"])
	}
}

func TestBuildFeishuImageFigure(t *testing.T) {
	figure := BuildFeishuImageFigure("系统链路图", "img_v2_67890", "链路图", "图 1.1: 投递链路分析")
	if len(figure) != 3 {
		t.Fatalf("expected 3 elements in figure (title, img, caption), got %d", len(figure))
	}

	if figure[0]["tag"] != "div" {
		t.Errorf("expected first element tag div, got %v", figure[0]["tag"])
	}
	if figure[1]["tag"] != "img" {
		t.Errorf("expected second element tag img, got %v", figure[1]["tag"])
	}
	if figure[2]["tag"] != "note" {
		t.Errorf("expected third element tag note, got %v", figure[2]["tag"])
	}
}
