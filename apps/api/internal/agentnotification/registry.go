package agentnotification

// Kind is an exact native operation, not a model-selected category. Registry
// metadata is fixed; callers cannot supply labels, recipients or source facts.
type Kind string

const (
	KindOpportunityAvailable         Kind = "opportunity_available"
	KindActivityReminder             Kind = "activity_reminder"
	KindActivityChange               Kind = "activity_change"
	KindActivityCancelled            Kind = "activity_cancelled"
	KindActivityReview               Kind = "activity_review"
	KindPlaceReview                  Kind = "place_review"
	KindDirectMessage                Kind = "direct_message"
	KindConnectionRequest            Kind = "connection_request"
	KindConnectionDecision           Kind = "connection_decision"
	KindCommunityMessage             Kind = "community_message"
	KindActivityMessage              Kind = "activity_message"
	KindOrganizationInvitation       Kind = "organization_invitation"
	KindOrganizationMembershipChange Kind = "organization_membership_change"
	KindAgentTaskCompleted           Kind = "agent_task_completed"
	KindAgentTaskFailed              Kind = "agent_task_failed"
	KindBusinessClaimReview          Kind = "business_claim_review"
	// Original explicit public publication only; Business Agent runtime remains unavailable.
	KindBusinessUpdate Kind = "business_update"
)

type Descriptor struct {
	Kind           Kind
	Category       Category
	LegacyCategory string
	Title          string
	Detail         string
}

func (Kind) MarshalJSON() ([]byte, error)       { return nil, ErrServerOnly }
func (kind *Kind) UnmarshalJSON([]byte) error   { *kind = ""; return ErrServerOnly }
func (Descriptor) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (descriptor *Descriptor) UnmarshalJSON([]byte) error {
	*descriptor = Descriptor{}
	return ErrServerOnly
}

func LookupKind(kind Kind) (Descriptor, error) {
	d := Descriptor{Kind: kind, LegacyCategory: "updates"}
	switch kind {
	case KindOpportunityAvailable:
		d.Category, d.Title, d.Detail = CategoryActivity, "与你的明确意图匹配", "有活动与你的城市和明确条件强规则匹配，请查看当前详情。匹配不代表已报名。"
	case KindActivityReminder:
		d.Category, d.Title, d.Detail = CategoryActivity, "活动即将开始", "你已报名的活动将在两小时内开始，请查看最新安排。"
	case KindActivityChange:
		d.Category, d.Title, d.Detail = CategoryActivity, "活动安排已调整", "你已报名的活动安排已变更，请查看最新详情。"
	case KindActivityCancelled:
		d.Category, d.Title, d.Detail, d.LegacyCategory = CategoryActivity, "活动已取消", "你已报名的活动已由主办方取消，请查看最新详情。", "needs_attention"
	case KindActivityReview:
		d.Category, d.Title, d.Detail = CategoryActivity, "活动建议审核结果", "你的活动建议审核状态已更新，请查看记录。"
	case KindPlaceReview:
		d.Category, d.Title, d.Detail = CategorySystem, "地点建议审核结果", "你的地点建议审核状态已更新，请查看记录。"
	case KindDirectMessage:
		d.Category, d.Title, d.Detail, d.LegacyCategory = CategoryMessage, "收到新消息", "打开对话查看消息。", "messages"
	case KindConnectionRequest:
		d.Category, d.Title, d.Detail, d.LegacyCategory = CategorySocial, "收到联系申请", "有新的联系申请，请查看并决定是否接受。", "requests"
	case KindConnectionDecision:
		d.Category, d.Title, d.Detail = CategorySocial, "联系申请已处理", "你的联系申请状态已更新，请查看记录。"
	case KindCommunityMessage:
		d.Category, d.Title, d.Detail, d.LegacyCategory = CategoryCommunity, "社区有新消息", "打开社区会话查看消息。", "messages"
	case KindActivityMessage:
		d.Category, d.Title, d.Detail, d.LegacyCategory = CategoryMessage, "活动会话有新消息", "打开活动会话查看消息。", "messages"
	case KindOrganizationInvitation:
		d.Category, d.Title, d.Detail, d.LegacyCategory = CategoryOrganization, "收到组织邀请", "有新的组织成员邀请，请查看并决定是否加入。", "requests"
	case KindOrganizationMembershipChange:
		d.Category, d.Title, d.Detail = CategoryOrganization, "组织成员资格已更新", "你在组织中的成员状态已更新，请查看当前权限。"
	case KindAgentTaskCompleted:
		d.Category, d.Title, d.Detail, d.LegacyCategory = CategoryAgent, "任务已完成", "你提交的任务状态已更新，请查看当前结果。", "agent_updates"
	case KindAgentTaskFailed:
		d.Category, d.Title, d.Detail, d.LegacyCategory = CategoryAgent, "任务暂未完成", "你提交的任务暂未完成，请查看并重试。", "agent_updates"
	case KindBusinessClaimReview:
		d.Category, d.Title, d.Detail = CategoryBusiness, "商家经营权审核状态已更新", "请在商家工作台查看当前审核状态。经营权核验不代表交易背书。"
	case KindBusinessUpdate:
		d.Category, d.Title, d.Detail = CategoryBusiness, "关注的商家公开资料已更新", "请查看当前公开资料。资料更新不代表可预订或已开启智能体。"
	default:
		return Descriptor{}, ErrInvalid
	}
	return d, nil
}
