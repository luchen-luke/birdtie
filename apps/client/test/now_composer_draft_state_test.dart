import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/now_composer_draft_state.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _alex = HumanConversation(id:'conversation-a',otherAccountId:'alex',otherName:'Alex');
const _river = HumanConversation(id:'conversation-b',otherAccountId:'river',otherName:'River');
void main() {
  test('Agent and each human recipient retain separate editable values', () {
    final d=NowComposerDraftState()..observeOwner('owner-a');
    const agent=TextEditingValue(text:'阿伯丁餐厅',selection:TextSelection.collapsed(offset:2));
    d.update(agent);
    final agentIdentity=d.identity;
    d.select(NowComposerTarget.human(_alex));
    expect(d.value,TextEditingValue.empty);
    d.update(const TextEditingValue(text:'发给Alex'));
    d.select(NowComposerTarget.human(_river));
    d.update(const TextEditingValue(text:'发给River'));
    d.select(NowComposerTarget.human(_alex));
    expect(d.value.text,'发给Alex');
    d.clearSent(agentIdentity);
    expect(d.value.text,'发给Alex');
    d.select(const NowComposerTarget.agent());
    expect(d.value,agent);
  });
  test('rejected session suspends then restores only the same owner, never sends', () {
    final d=NowComposerDraftState()..observeOwner('owner-a');
    d.update(const TextEditingValue(text:'未发送内容'));
    final original=d.identity;
    d.observeOwner(null,rejectedOwner:'owner-a');
    expect(d.suspended,isTrue);
    d.update(TextEditingValue.empty);
    d.clearSent(original);
    expect(d.value.text,'未发送内容');
    d.observeOwner('owner-a');
    expect(d.suspended,isFalse);
    expect(d.value.text,'未发送内容');
    expect(d.identity,isNot(original));
    d.observeOwner(null,rejectedOwner:'owner-a');
    d.observeOwner('owner-b');
    expect(d.value,TextEditingValue.empty);
    d.observeOwner('owner-a');
    expect(d.value,TextEditingValue.empty);
  });
  test('explicit logout retires private material; first login may keep guest input', () {
    final d=NowComposerDraftState();
    d.update(const TextEditingValue(text:'游客未发问题'));
    d.observeOwner('owner-a');
    expect(d.value.text,'游客未发问题');
    d.observeOwner(null);
    expect(d.value,TextEditingValue.empty);
    d.observeOwner('owner-a');
    expect(d.value,TextEditingValue.empty);
  });
  test('late success never clears a newer edit or another recipient', () {
    final d=NowComposerDraftState()..observeOwner('owner-a');
    d.update(const TextEditingValue(text:'原提交'));
    final identity=d.identity;
    d.update(const TextEditingValue(text:'下一句'));
    d.clearSent(identity,expectedText:'原提交');
    expect(d.value.text,'下一句');
  });
}
