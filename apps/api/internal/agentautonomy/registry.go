// Package agentautonomy defines finite server-owned preparation limits.
// Settings and offline classifications do not authorize data reads or actions.
package agentautonomy

type Level string
type Operation string

const (
	LevelObserve         Level     = "LEVEL_0_OBSERVE"
	LevelAssist          Level     = "LEVEL_1_ASSIST"
	LevelPrepare         Level     = "LEVEL_2_PREPARE"
	LevelDelegate        Level     = "LEVEL_3_DELEGATE"
	Observe              Operation = "OBSERVE"
	Understand           Operation = "UNDERSTAND"
	BuildContext         Operation = "BUILD_CONTEXT"
	Summarize            Operation = "SUMMARIZE"
	Recommend            Operation = "RECOMMEND"
	Prioritize           Operation = "PRIORITIZE"
	Remind               Operation = "REMIND"
	DraftResponse        Operation = "DRAFT_RESPONSE"
	PrepareInvitation    Operation = "PREPARE_INVITATION"
	PrepareRegistration  Operation = "PREPARE_REGISTRATION"
	SuggestMeeting       Operation = "SUGGEST_MEETING"
	TakeAutonomousAction Operation = "TAKE_AUTONOMOUS_ACTION"
)

// Descriptor may be displayed as ordinary JSON. It is a closed catalogue,
// never a tool, current source resolver, approval or execution result.
type Descriptor struct {
	Operation              Operation `json:"operation"`
	MinimumLevel           Level     `json:"minimumLevel"`
	NeedsHumanConfirmation bool      `json:"needsHumanConfirmation"`
	NeedsSocialPolicy      bool      `json:"needsSocialPolicy"`
	Explanation            string    `json:"explanation"`
}

var catalogue = [...]Descriptor{
	{Observe, LevelObserve, false, false, "观察已获准的信息；自主性设置不授予读取权限。"},
	{Understand, LevelObserve, false, false, "理解已获准的信息；未知事实仍保留未知。"},
	{BuildContext, LevelObserve, false, false, "整理当前获准的上下文；不能复制私人内容或补造同意。"},
	{Summarize, LevelAssist, false, false, "总结已获准的信息；总结不是新事实或提交结果。"},
	{Recommend, LevelAssist, false, false, "给出建议；不能自动接受邀请或报名。"},
	{Prioritize, LevelAssist, false, false, "排列已获准的事项；排序不扩大权限。"},
	{Remind, LevelAssist, false, false, "提出提醒建议；实际提醒仍使用原通知领域与当前权限。"},
	{DraftResponse, LevelPrepare, true, true, "形成回复草稿；具体版本必须由本人检查确认，不能自动发送。"},
	{PrepareInvitation, LevelPrepare, true, true, "准备邀请草稿；具体收件人和版本必须由本人检查确认。"},
	{PrepareRegistration, LevelPrepare, true, false, "准备报名草稿；具体活动和版本必须由本人检查确认，不能自动报名。"},
	{SuggestMeeting, LevelPrepare, true, true, "准备见面建议；具体对象、时间和版本必须由本人检查确认。"},
	{TakeAutonomousAction, LevelDelegate, false, false, "自主行动层级仅保留定义；当前版本禁止配置与执行。"},
}

func Levels() []Level          { return []Level{LevelObserve, LevelAssist, LevelPrepare, LevelDelegate} }
func Operations() []Descriptor { return append([]Descriptor(nil), catalogue[:]...) }
func Lookup(operation Operation) (Descriptor, bool) {
	for _, descriptor := range catalogue {
		if descriptor.Operation == operation {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}
func levelRank(level Level) (int, bool) {
	for index, candidate := range Levels() {
		if level == candidate {
			return index, true
		}
	}
	return 0, false
}
