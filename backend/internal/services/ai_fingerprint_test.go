package services

import "testing"

func TestDetectAIFingerprintIssuesCatchesForumPatterns(t *testing.T) {
	text := `他看了一眼她，心跳漏了一拍。
空气中弥漫着消毒水的气味，只剩空调嗡鸣。
这不是害怕，而是某种说不清的预感。
他沉默了三秒，又愣了一下。
他知道，这只是开始。`

	issues, penalty := detectAIFingerprintIssues(text, 0)
	if len(issues) < 3 {
		t.Fatalf("expected multiple fingerprint issues, got %d: %#v", len(issues), issues)
	}
	if penalty <= 0 {
		t.Fatalf("expected positive penalty, got %.2f", penalty)
	}

	foundEnding := false
	for _, issue := range issues {
		if issue.Message != "" && issue.Severity == "critical" {
			foundEnding = true
			break
		}
	}
	if !foundEnding {
		t.Fatalf("expected critical AI ending issue, got %#v", issues)
	}
}

func TestSentenceLengthBurstinessDetectsUniformSentences(t *testing.T) {
	text := `他推开房门走进屋里。她低头看着桌上茶杯。他拿起纸条看了一眼。她站在窗边没有说话。他把纸条放回桌面。她抬头望着他的眼睛。他转身走向门口。她终于开口叫住了他。`

	issues, _ := detectAIFingerprintIssues(text, 0)
	for _, issue := range issues {
		if issue.Message != "" && issue.Type == "ai_fingerprint" {
			return
		}
	}
	t.Fatalf("expected structural fingerprint issue, got none")
}
