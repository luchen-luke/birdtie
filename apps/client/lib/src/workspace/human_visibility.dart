/// Ordinary human profile audiences. These never authorize Agent context,
/// model egress, private messages, or long-term Memory.
const humanVisibilityLabels = {
  'public': '公开',
  'followers': '关注我的人',
  'connections': '好友',
  'close': '亲密好友',
  'private': '仅自己',
};

const humanVisibilityDescriptions = {
  'public': '任何人都可能看到你的名称和简介；公开资料也可出现在人员搜索结果中。',
  'followers': '当前关注你的人可以查看；取消关注后不再按此范围可见。',
  'connections': '当前已互相接受的好友可以查看；移除好友后不再按此范围可见。',
  'close': '仅你明确选中的当前亲密好友可以查看；名单可随时修改。',
  'private': '默认仅自己可见；仍有效的明确资料授权保持原有范围，可在授权管理中撤销。',
};

String humanVisibility(Object? value) {
  if (value is! String || !humanVisibilityLabels.containsKey(value)) {
    throw const FormatException('个人资料可见范围无法核实');
  }
  return value;
}

const intentAudienceDescriptions = {
  'PRIVATE': '仅自己可见，不参与人员发现。',
  'FOLLOWERS': '当前关注你的人可以发现这条意图。',
  'CONNECTIONS': '当前已互相接受的好友可以发现这条意图。',
  'FRIENDS': '当前已互相接受的好友可以发现这条意图（原好友范围）。',
  'CLOSE': '仅你明确选中的当前亲密好友可以发现；移除或屏蔽后不再可见。',
  'PUBLIC': '所有符合访问条件的人都可能看到这条意图。',
  'LOCAL': '当前公开城市范围内的用户可以发现这条意图。',
  'COMMUNITY': '所选社群的当前成员可以发现这条意图。',
  'INVITE_ONLY': '仅所选邀请对象可见；开启意图不会自动发送邀请。',
};
