// Package domain 承载全部业务规则：状态派生、卡点、互锁、审批链、权限、
// 进度、五组归类。规则只在此包实现，API handler 保持薄层，前端只消费派生字段。
//
// 实现顺序遵循项目 CLAUDE.md 的 coding 流程：先按 PRD §12 验收场景（AC-01～AC-49）
// 写表驱动单测，再实现规则。
package domain
