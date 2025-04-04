// internal/processor/rules.go
package processor

import (
	"sort"
)

// ApplyDecisionRules 应用决策规则
// 规则如下:
// 1. 如果有拦截决策，优先采用拦截决策
// 2. 如果多个拦截决策，采用最先收到的
// 3. 如果只有放行决策，采用最先收到的
// 4. 如果没有决策，默认放行
func ApplyDecisionRules(decisions []*Decision) *Decision {
	if len(decisions) == 0 {
		// 无决策，创建默认的放行决策
		return &Decision{
			Action: ActionAllow,
			Source: DecisionSource{
				ID: "system",
			},
			Reason: "无决策，默认放行",
		}
	}

	// 如果只有一个决策，直接返回
	if len(decisions) == 1 {
		return decisions[0]
	}

	// 查找所有拦截决策
	var intercepts []*Decision
	for _, d := range decisions {
		if d.Action == ActionIntercept {
			intercepts = append(intercepts, d)
		}
	}

	// 如果有拦截决策，选择最早的
	if len(intercepts) > 0 {
		return findEarliestDecision(intercepts)
	}

	// 否则选择最早的放行决策
	return findEarliestDecision(decisions)
}

// findEarliestDecision 查找最早的决策
func findEarliestDecision(decisions []*Decision) *Decision {
	if len(decisions) == 0 {
		return nil
	}

	if len(decisions) == 1 {
		return decisions[0]
	}

	// 复制一份决策列表
	copied := make([]*Decision, len(decisions))
	copy(copied, decisions)

	// 按时间戳排序
	sort.Slice(copied, func(i, j int) bool {
		return copied[i].Source.Timestamp.Before(copied[j].Source.Timestamp)
	})

	return copied[0]
}
