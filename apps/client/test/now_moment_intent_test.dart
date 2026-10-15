import 'package:flutter_test/flutter_test.dart';
import '../lib/src/workspace/now_moment_intent.dart';

void main() {
  test('only an explicit compose instruction extracts the selected text', () {
    expect(explicitMomentDraftBody('帮我发条动态：今天去海边\n风很大'), '今天去海边\n风很大');
    expect(explicitMomentDraftBody('帮我发一条动态'), '');
    expect(explicitMomentDraftBody('发布动态，我以后想去巴黎'), '我以后想去巴黎');
    expect(explicitMomentDraftBody('他让我帮我发条动态：你好'), isNull);
    expect(explicitMomentDraftBody('怎么发布动态？'), isNull);
    expect(explicitMomentDraftBody('今天想去巴黎'), isNull);
  });
}
