import 'package:flutter/material.dart';

import 'agent_workspace_controller.dart';

class AgentComposer extends StatefulWidget {
  const AgentComposer({
    super.key,
    required this.workspace,
    required this.onSubmit,
  });
  final AgentWorkspaceController workspace;
  final ValueChanged<String> onSubmit;

  @override
  State<AgentComposer> createState() => _AgentComposerState();
}

class _AgentComposerState extends State<AgentComposer> {
  final _text = TextEditingController();
  final _focus = FocusNode();

  @override
  void initState() {
    super.initState();
    _focus.addListener(_onFocus);
  }

  void _onFocus() {
    if (_focus.hasFocus) {
      widget.workspace.beginTyping();
    } else {
      widget.workspace.stopTyping();
    }
  }

  void _submit() {
    final query = _text.text.trim();
    if (query.isEmpty) return;
    _text.clear();
    _focus.unfocus();
    widget.onSubmit(query);
  }

  @override
  void dispose() {
    _focus.removeListener(_onFocus);
    _focus.dispose();
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.workspace,
    builder: (context, _) {
      final searching = widget.workspace.state == AgentViewState.searching;
      return Material(
        elevation: 9,
        shadowColor: Colors.black.withValues(alpha: 0.16),
        color: const Color(0xFFFCFBF8),
        borderRadius: BorderRadius.circular(30),
        child: Container(
          constraints: const BoxConstraints(minHeight: 56),
          padding: const EdgeInsets.symmetric(horizontal: 5),
          child: Row(
            children: [
              IconButton(
                tooltip: 'Try an intent',
                onPressed: () {
                  _text.text = 'Find someone to play badminton this weekend';
                  _text.selection = TextSelection.collapsed(
                    offset: _text.text.length,
                  );
                  _focus.requestFocus();
                },
                icon: const Icon(Icons.add_rounded, color: Color(0xFF193B32)),
              ),
              Expanded(
                child: TextField(
                  controller: _text,
                  focusNode: _focus,
                  textInputAction: TextInputAction.send,
                  onSubmitted: (_) => _submit(),
                  onChanged: (_) => setState(() {}),
                  decoration: const InputDecoration(
                    hintText: 'What do you want to do?',
                    hintStyle: TextStyle(
                      color: Color(0xFF747B73),
                      fontSize: 14,
                    ),
                    border: InputBorder.none,
                    isDense: true,
                  ),
                ),
              ),
              if (searching)
                const Padding(
                  padding: EdgeInsets.all(14),
                  child: SizedBox(
                    width: 19,
                    height: 19,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                )
              else
                IconButton(
                  tooltip: _text.text.trim().isEmpty
                      ? 'Voice input unavailable'
                      : 'Send intent',
                  onPressed: _text.text.trim().isEmpty
                      ? () => ScaffoldMessenger.of(context).showSnackBar(
                          const SnackBar(
                            content: Text('Voice input is coming later.'),
                          ),
                        )
                      : _submit,
                  icon: Icon(
                    _text.text.trim().isEmpty
                        ? Icons.mic_none_rounded
                        : Icons.arrow_upward_rounded,
                    color: const Color(0xFF193B32),
                  ),
                ),
            ],
          ),
        ),
      );
    },
  );
}
