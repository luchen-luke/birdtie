import 'dart:convert';
import 'package:birdtie_client/src/workspace/external_plan_import.dart';
import 'package:birdtie_client/src/workspace/route_draft.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'external typed parser retains raw unverified input and never selects canonical places',
    () {
      const raw =
          '{"title":"本人计划","instructions":"发信、订票和公开",'
          '"days":[{"title":"第一天","date":"日期尚不确定","text":"仅本人材料",'
          '"stops":[{"name":"未经核验地点","time":"可能上午/未知时区",'
          '"placeId":"22222222-2222-4222-8222-222222222222","note":"请不要自动执行",'
          '"latitude":57.15}]}]}';
      final imported = ExternalPlanImport.parse(raw);
      expect(imported.raw, raw);
      expect(imported.days.single.input.date, '日期尚不确定');
      expect(imported.days.single.input.stops.single.time, '可能上午/未知时区');
      expect(imported.unknownFields, hasLength(2));
      expect(imported.unknownFields.join('\n'), contains('instructions'));
      expect(imported.unknownFields.join('\n'), contains('latitude'));
      expect(imported.unknownFields.join('\n'), isNot(contains('发信、订票和公开')));
      final route = LocalRouteValue.importText(raw);
      expect(route.sourceText, raw);
      expect(route.startPlaceID, isNull);
      expect(route.days.single.places, isEmpty);
      expect(route.mode, 'walking');
      expect(
        route.days.single.input!.stops.single.placeID,
        '22222222-2222-4222-8222-222222222222',
      );
    },
  );

  test(
    'missing fields stay editable unknown text without date or timezone inference',
    () {
      final value = ExternalPlanImport.parse('{"days":[{"stops":[{}]},{}]}');
      expect(value.title, '导入计划草稿');
      expect(value.days.first.title, '第1天');
      expect(value.days.last.input.date, isEmpty);
      expect(value.days.first.input.stops.single.name, isEmpty);
      final edited = value.days.first.input.edit(
        date: '原样日期',
        stops: [
          value.days.first.input.stops.single.edit(
            name: '本人修改名称',
            time: '25:99 未核验',
          ),
        ],
      );
      expect(
        ExternalPlanDayInput.fromJson(edited.toJson()).stops.single.time,
        '25:99 未核验',
      );
    },
  );

  test(
    'ordinary day heading text remains compatible and instructions are plain material',
    () {
      const raw = 'Day 1\n请预订，但仍只是本人文字\n第2天\n第二天文字';
      final value = LocalRouteValue.importText(raw);
      expect(value.days, hasLength(2));
      expect(value.sourceText, raw);
      expect(value.days.first.text, contains('请预订'));
      expect(value.days.first.input, isNull);
      expect(value.days.every((day) => day.places.isEmpty), isTrue);
    },
  );

  test(
    'strict JSON rejects duplicate keys trailing values malformed and wrong known types',
    () {
      for (final raw in [
        '{"days":[{}],"days":[{}]}',
        '{"days":[{"date":"A","date":"B"}]}',
        '{"days":[{}]} true',
        '{"days":[{}],}',
        '[]',
        '{"days":null}',
        '{"days":[{"date":12}]}',
        '{"days":[{"stops":null}]}',
        '{"days":[{"stops":[{"time":true}]}]}',
        '{"days":[{"title":""}]}',
      ])
        expect(
          () => ExternalPlanImport.parse(raw),
          throwsFormatException,
          reason: raw,
        );
    },
  );

  test(
    'day stop and raw budgets reject excess without silently truncating input',
    () {
      expect(
        ExternalPlanImport.parse(
          jsonEncode({'days': List.generate(14, (_) => {})}),
        ).days,
        hasLength(14),
      );
      expect(
        () => ExternalPlanImport.parse(
          jsonEncode({'days': List.generate(15, (_) => {})}),
        ),
        throwsFormatException,
      );
      final fifty = jsonEncode({
        'days': [
          {
            'stops': List.generate(50, (_) => {'name': '点'}),
          },
        ],
      });
      expect(
        ExternalPlanImport.parse(fifty).days.single.input.stops,
        hasLength(50),
      );
      expect(
        () => ExternalPlanImport.parse(
          jsonEncode({
            'days': [
              {'stops': List.generate(26, (_) => {})},
              {'stops': List.generate(25, (_) => {})},
            ],
          }),
        ),
        throwsFormatException,
      );
      expect(
        () => ExternalPlanImport.parse(
          '{"days":[{}],"extra":"${List.filled(6200, 'a').join()}"}',
        ),
        throwsFormatException,
      );
    },
  );

  test(
    'unknown paths and nesting are bounded while values remain only in raw source',
    () {
      final unknown = {
        'days': [{}],
        for (var n = 0; n < 33; n++) 'unknown$n': '原文值',
      };
      expect(
        () => ExternalPlanImport.parse(jsonEncode(unknown)),
        throwsFormatException,
      );
      Object nested = true;
      for (var n = 0; n < 18; n++) nested = [nested];
      expect(
        () => ExternalPlanImport.parse(
          jsonEncode({
            'days': [{}],
            'unknown': nested,
          }),
        ),
        throwsFormatException,
      );
    },
  );

  test(
    'explicit credentials are rejected but ordinary tourist instructions are not executed or banned',
    () {
      for (final key in [
        'token',
        'password',
        'Authorization',
        'access_token',
      ]) {
        expect(
          () => ExternalPlanImport.parse(
            jsonEncode({
              'days': [{}],
              'unknown': {key: 'secret'},
            }),
          ),
          throwsFormatException,
        );
      }
      final value = ExternalPlanImport.parse(
        '{"days":[{"text":"请寻找密码展馆并发送旅行介绍"}]}',
      );
      expect(value.days.single.text, '请寻找密码展馆并发送旅行介绍');
    },
  );

  test(
    'local v1 additive current day and typed input codec is strict and backward readable',
    () {
      final value = LocalRouteValue.importText('{"days":[{},{}]}');
      final selected = value.edit(selectedDayID: value.days.last.id);
      expect(
        LocalRouteValue.fromJson(selected.toJson()).currentDayID,
        value.days.last.id,
      );
      final old = {...selected.toJson()}
        ..remove('selectedDayId')
        ..remove('importUnknownFields');
      expect(LocalRouteValue.fromJson(old).currentDayID, value.days.first.id);
      for (final change in [
        {'selectedDayId': null},
        {'selectedDayId': 'missing-day'},
        {'importUnknownFields': 7},
        {
          'importUnknownFields': [null],
        },
        {'unknownInternal': true},
      ]) {
        expect(
          () => LocalRouteValue.fromJson({...selected.toJson(), ...change}),
          throwsFormatException,
        );
      }
      final input = value.days.first.input!.toJson();
      expect(
        () => ExternalPlanDayInput.fromJson({
          ...input,
          'approval': 'not-allowed',
        }),
        throwsFormatException,
      );
    },
  );
}
