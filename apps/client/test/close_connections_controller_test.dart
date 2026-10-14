import 'dart:async';
import 'package:birdtie_client/src/workspace/close_connections_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'close_connections_api_test.dart';

void main() {
  test('DELETE后另一个当前修改不能被GET覆盖说成已撤销', () async {
    var deleted=false;
    final api=closeAPI((r)async{
      if(r.method=='DELETE'){deleted=true;return http.Response('',204);}
      return closeJSON(closeList(entries:[closeEntry(selected:true,revision:deleted?3:1)]));
    });
    final c=CloseConnectionsController(api:api,identity:()=>const CloseConnectionsIdentity('Bearer A',closeOwner,null),now:()=>closeNow);
    addTearDown(c.dispose);await c.load();final old=c.list!.connections.single;
    await c.confirm(old,add:false,epoch:c.generation,deadline:c.list!.validUntil);
    expect(c.list!.connections.single.isClose,true);expect(c.message,contains('当前仍在'));
    expect(c.message,isNot(contains('已撤销这位')));expect(c.ready,true);
  });
  test('读取和取消预览不会写，具体人工确认一次PUT再GET核对', () async {
    final calls = <String>[];
    var selected = false;
    final api = closeAPI((r) async {
      calls.add(r.method);
      if (r.method == 'PUT') {
        selected = true;
        return closeJSON(closeResult());
      }
      return closeJSON(closeList(entries: [closeEntry(selected: selected)]));
    });
    final c = CloseConnectionsController(
      api: api,
      identity: () =>
          const CloseConnectionsIdentity('Bearer synthetic', closeOwner, null),
      now: () => closeNow,
    );
    addTearDown(c.dispose);
    await c.load();
    expect(c.ready, true);
    expect(calls, ['GET']);
    final entry = c.list!.connections.single,
        deadline = c.list!.validUntil,
        epoch = c.generation;
    expect(c.reviewCurrent(entry, epoch, deadline), true);
    expect(calls, ['GET']);
    await c.confirm(entry, add: true, epoch: epoch, deadline: deadline);
    expect(calls, ['GET', 'PUT', 'GET']);
    expect(c.list!.connections.single.isClose, true);
    expect(c.unknown, false);
    await c.confirm(entry, add: true, epoch: epoch, deadline: deadline);
    expect(calls, ['GET', 'PUT', 'GET']);
  });
  test('未知PUT停止提交，恢复只GET最新名单不声称历史成功', () async {
    final calls = <String>[];
    final api = closeAPI((r) async {
      calls.add(r.method);
      if (r.method == 'PUT') throw TimeoutException('SYNTHETIC');
      return closeJSON(closeList());
    });
    final c = CloseConnectionsController(
      api: api,
      identity: () =>
          const CloseConnectionsIdentity('Bearer synthetic', closeOwner, null),
      now: () => closeNow,
    );
    addTearDown(c.dispose);
    await c.load();
    final entry = c.list!.connections.single, deadline = c.list!.validUntil;
    await c.confirm(entry, add: true, epoch: c.generation, deadline: deadline);
    expect(c.unknown, true);
    expect(c.ready, false);
    await c.confirm(entry, add: true, epoch: c.generation, deadline: deadline);
    expect(calls, ['GET', 'PUT']);
    await c.load();
    expect(calls, ['GET', 'PUT', 'GET']);
    expect(c.unknown, false);
    expect(c.message, contains('不能证明'));
  });
  test('DELETE204后读取失败保留未核实并禁动作，重新GET可撤销', () async {
    var gets = 0;
    final calls = <String>[];
    final api = closeAPI((r) async {
      calls.add(r.method);
      if (r.method == 'DELETE') return http.Response('', 204);
      gets++;
      if (gets == 2) return http.Response('', 503);
      return closeJSON(closeList(entries: [closeEntry(selected: gets == 1)]));
    });
    final c = CloseConnectionsController(
      api: api,
      identity: () =>
          const CloseConnectionsIdentity('Bearer synthetic', closeOwner, null),
      now: () => closeNow,
    );
    addTearDown(c.dispose);
    await c.load();
    final old = c.list!.connections.single;
    await c.confirm(
      old,
      add: false,
      epoch: c.generation,
      deadline: c.list!.validUntil,
    );
    expect(c.unknown, true);
    expect(c.message, contains('操作已回应'));
    expect(c.ready, false);
    await c.load();
    expect(c.list!.connections.single.isClose, false);
    expect(calls, ['GET', 'DELETE', 'GET', 'GET']);
  });
  for (final mode in ['account', 'token', 'workspace']) {
    for (final write in [false, true]) {
      test('实际$mode ABA迟到${write ? 'PUT' : 'GET'}不能复用旧批准或泄露旧名单', () async {
        var who = const CloseConnectionsIdentity('Bearer A', closeOwner, null);
        final late = Completer<http.Response>();
        var gets = 0;
        final api = closeAPI((r) async {
          if (r.method == 'PUT') return late.future;
          gets++;
          return !write ? late.future : closeJSON(closeList());
        });
        final c = CloseConnectionsController(
          api: api,
          identity: () => who,
          now: () => closeNow,
        );
        addTearDown(c.dispose);
        Future<void> work;
        if (write) {
          await c.load();
          final old = c.list!.connections.single;
          work = c.confirm(
            old,
            add: true,
            epoch: c.generation,
            deadline: c.list!.validUntil,
          );
        } else {
          work = c.load();
        }
        await Future<void>.delayed(Duration.zero);
        who = switch (mode) {
          'account' => const CloseConnectionsIdentity(
            'Bearer B',
            closePeer,
            null,
          ),
          'token' => const CloseConnectionsIdentity(
            'Bearer B',
            closeOwner,
            null,
          ),
          _ => const CloseConnectionsIdentity('Bearer A', closeOwner, closeTie),
        };
        c.sync();
        who = const CloseConnectionsIdentity('Bearer A', closeOwner, null);
        c.sync();
        late.complete(closeJSON(write ? closeResult() : closeList()));
        await work;
        expect(c.current, false);
        expect(c.list, isNull);
        expect(c.generation, 2);
        expect(gets, write ? 1 : 1);
        await c.load();
        expect(gets, 1);
      });
    }
  }
  test('截止、换名单及409不会复用旧对象', () async {
    var clock = closeNow;
    final calls = <String>[];
    final api = closeAPI((r) async {
      calls.add(r.method);
      return r.method == 'PUT'
          ? http.Response('', 409)
          : closeJSON(closeList(now: clock));
    }, now: () => clock);
    final c = CloseConnectionsController(
      api: api,
      identity: () =>
          const CloseConnectionsIdentity('Bearer A', closeOwner, null),
      now: () => clock,
    );
    addTearDown(c.dispose);
    await c.load();
    final old = c.list!.connections.single, deadline = c.list!.validUntil;
    clock = deadline;
    expect(c.ready, false);
    await c.confirm(old, add: true, epoch: c.generation, deadline: deadline);
    expect(calls, ['GET']);
    await c.load();
    expect(c.reviewCurrent(old, c.generation, c.list!.validUntil), false);
    await c.confirm(
      c.list!.connections.single,
      add: true,
      epoch: c.generation,
      deadline: c.list!.validUntil,
    );
    expect(c.unknown, true);
    expect(c.message, contains('版本已变化'));
    expect(calls, ['GET', 'GET', 'PUT']);
  });
  test('重复点击同确认只一个原写，空名单可读且没有私有数据', () async {
    final hold = Completer<http.Response>();
    var writes = 0;
    final api = closeAPI((r) async {
      if (r.method == 'PUT') {
        writes++;
        return hold.future;
      }
      return closeJSON(closeList(entries: writes > 0 ? [] : null));
    });
    final c = CloseConnectionsController(
      api: api,
      identity: () =>
          const CloseConnectionsIdentity('Bearer A', closeOwner, null),
      now: () => closeNow,
    );
    addTearDown(c.dispose);
    await c.load();
    final e = c.list!.connections.single,
        d = c.list!.validUntil,
        g = c.generation;
    final pending = c.confirm(e, add: true, epoch: g, deadline: d);
    await c.confirm(e, add: true, epoch: g, deadline: d);
    await Future<void>.delayed(Duration.zero);
    expect(writes, 1);
    hold.complete(closeJSON(closeResult()));
    await pending;
    expect(c.list!.connections, isEmpty);
    expect(c.message, contains('当前关系已变化'));
  });
}
