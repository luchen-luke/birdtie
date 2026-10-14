import 'dart:convert';
import 'package:crypto/crypto.dart';

/// A RAM-only handoff. The receiver may save the checked text in an existing
/// personal local draft; these selectors never grant a writer or model action.
enum LocalVisualDraftStatus { ownQuestion, unverifiedReportNote }

final _visualID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final _visualHash = RegExp(r'^[0-9a-f]{64}$');
bool _id(String v) =>
    _visualID.hasMatch(v) && v != '00000000-0000-0000-0000-000000000000';
bool _hash(String v) => _visualHash.hasMatch(v) && v != '0' * 64;
const localVisualTextMaxBytes = 8000;
String _digest(Object value) =>
    sha256.convert(utf8.encode(jsonEncode(value))).toString();
bool _version(int v) => v > 0 && v <= 8999999999999999;
String _checkedText(
  String text, {
  bool empty = false,
  int max = localVisualTextMaxBytes,
}) {
  final clean = text.trim();
  if ((!empty && clean.isEmpty) ||
      clean.contains('\u0000') ||
      utf8.encode(clean).length > max)
    throw const FormatException('本机文字为空或过长，请先编辑。');
  return clean;
}

class LocalVisualSourceSelection {
  LocalVisualSourceSelection({
    required this.ownerID,
    required this.origin,
    required this.sourceContextType,
    required this.sourceContextID,
    required this.clientDraftID,
    required this.clientDraftRevision,
    required this.selectedImageSHA256,
  }) {
    final uri = Uri.tryParse(origin);
    if (!_id(ownerID) ||
        uri == null ||
        !const {'http', 'https'}.contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.origin != origin ||
        uri.userInfo.isNotEmpty ||
        !const {'CITY', 'ONLINE'}.contains(sourceContextType) ||
        sourceContextID.trim().isEmpty ||
        utf8.encode(sourceContextID).length > 120 ||
        sourceContextID.contains(RegExp(r'[\x00-\x1f\x7f]')) ||
        sourceContextType == 'ONLINE' && !_id(sourceContextID) ||
        !_id(clientDraftID) ||
        !_version(clientDraftRevision) ||
        !_hash(selectedImageSHA256))
      throw const FormatException('本机图片草稿的本人或来源版本无效。');
  }
  final String ownerID,
      origin,
      sourceContextType,
      sourceContextID,
      clientDraftID,
      selectedImageSHA256;
  final int clientDraftRevision;
  String get bindingKey => _digest([
    'local-visual-selection-v1',
    ownerID,
    origin,
    sourceContextType,
    sourceContextID,
    clientDraftID,
    clientDraftRevision,
    selectedImageSHA256,
  ]);
  bool matches(String currentOwner, String currentOrigin) =>
      ownerID == currentOwner && origin == currentOrigin;
}

class LocalVisualReportProvenance {
  LocalVisualReportProvenance({
    required this.selectionBindingKey,
    required this.operationID,
    required this.previewID,
    required this.previewVersion,
    required this.pixelID,
    required this.pixelRevision,
    required this.pixelSHA256,
    required this.reportSHA256,
    required List<String> unknowns,
  }) : unknowns = List.unmodifiable(unknowns) {
    if (!_hash(selectionBindingKey) ||
        !_id(operationID) ||
        !_id(previewID) ||
        !_version(previewVersion) ||
        !_id(pixelID) ||
        !_version(pixelRevision) ||
        !_hash(pixelSHA256) ||
        !_hash(reportSHA256) ||
        unknowns.length > 32)
      throw const FormatException('图片报告的原来源尚未完整核对。');
    for (final value in unknowns) _checkedText(value, max: 2000);
  }
  final String selectionBindingKey,
      operationID,
      previewID,
      pixelID,
      pixelSHA256,
      reportSHA256;
  final int previewVersion, pixelRevision;
  final List<String> unknowns;
  String get bindingKey => _digest([
    'local-visual-report-v1',
    selectionBindingKey,
    operationID,
    previewID,
    previewVersion,
    pixelID,
    pixelRevision,
    pixelSHA256,
    reportSHA256,
  ]);
}

class LocalVisualDraft {
  LocalVisualDraft._({
    required this.text,
    required this.status,
    required this.sourceSelection,
    this.reportProvenance,
  });
  final String text;
  final LocalVisualDraftStatus status;
  final LocalVisualSourceSelection sourceSelection;
  final LocalVisualReportProvenance? reportProvenance;
  bool get includesUnverifiedObservation =>
      status == LocalVisualDraftStatus.unverifiedReportNote;
  String get contentVersion => _digest([
    'local-visual-text-v1',
    status.name,
    sourceSelection.bindingKey,
    reportProvenance?.bindingKey,
    text,
  ]);
  factory LocalVisualDraft.ownQuestion({
    required String text,
    required LocalVisualSourceSelection sourceSelection,
  }) => LocalVisualDraft._(
    text: _checkedText(text),
    status: LocalVisualDraftStatus.ownQuestion,
    sourceSelection: sourceSelection,
  );
  factory LocalVisualDraft.reportNote({
    required String text,
    required LocalVisualSourceSelection sourceSelection,
    required LocalVisualReportProvenance reportProvenance,
  }) {
    if (sourceSelection.bindingKey != reportProvenance.selectionBindingKey)
      throw const FormatException('原报告不属于当前选择的图片版本。');
    final note = _checkedText(text);
    final result =
        '未核验的图片观察摘录\n来源：本人已核对的图片分析报告；这段摘录仍需人工核实，不是地点事实、到访证据或记忆权威。\n$note${reportProvenance.unknowns.isEmpty ? '' : '\n尚不能确定：\n${reportProvenance.unknowns.map((v) => '· $v').join('\n')}'}';
    return LocalVisualDraft._(
      text: _checkedText(result),
      status: LocalVisualDraftStatus.unverifiedReportNote,
      sourceSelection: sourceSelection,
      reportProvenance: reportProvenance,
    );
  }
}
