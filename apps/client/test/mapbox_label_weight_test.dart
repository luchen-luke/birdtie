import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/city/mapbox_style.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('only explicit secondary labels receive a weight', () {
    expect(mapboxSecondaryLabelFactor('road-street-label', 'symbol'), 0.62);
    expect(
      mapboxSecondaryLabelFactor('settlement-subdivision-label', 'symbol'),
      0.56,
    );
    expect(
      mapboxSecondaryLabelFactor(
        'custom-label',
        'symbol',
        sourceLayer: 'neighborhood_label',
      ),
      0.56,
    );
    for (final id in [
      'road-label',
      'road-primary-label',
      'road-motorway-shield',
      'settlement-major-label',
      'settlement-minor-label',
      'country-label',
      'state-label',
      'water-label',
      'point-annotations',
      'unrecognized-label',
    ]) {
      expect(mapboxSecondaryLabelFactor(id, 'symbol'), isNull, reason: id);
    }
    expect(
      mapboxSecondaryLabelFactor(
        'road-minor-label',
        'symbol',
        hasIconImage: true,
      ),
      isNull,
    );
    expect(mapboxSecondaryLabelFactor('district-label', 'line'), isNull);
    expect(
      mapboxSecondaryLabelFactor(
        'custom',
        'symbol',
        sourceLayer: 'place_label',
      ),
      isNull,
    );
    expect(
      mapboxSecondaryLabelFactor(
        'district-label',
        'symbol',
        sourceLayer: 'poi_label',
      ),
      isNull,
    );
    expect(
      mapboxSecondaryLabelFactor(
        'district-label',
        'symbol',
        sourceLayer: 'water',
      ),
      isNull,
    );
  });

  test(
    'numeric opacity preserves existing fade and rejects invalid values',
    () {
      expect(mapboxWeightedTextOpacity(null, 0.62), 0.62);
      expect(mapboxWeightedTextOpacity(0, 0.62), 0);
      expect(mapboxWeightedTextOpacity(0.5, 0.62), 0.31);
      for (final original in [
        true,
        '1',
        -0.1,
        1.1,
        double.nan,
        double.infinity,
      ]) {
        expect(mapboxWeightedTextOpacity(original, 0.62), isNull);
      }
      for (final factor in [0.0, -1.0, 1.01, double.nan, double.infinity]) {
        expect(mapboxWeightedTextOpacity(1, factor), isNull);
      }
    },
  );

  test('zoom step/interpolate retain top-level input and original stops', () {
    final step = <Object>[
      'step',
      ['zoom'],
      0,
      10,
      0.5,
      15,
      1,
    ];
    final before = jsonEncode(step);
    expect(mapboxWeightedTextOpacity(step, 0.62), [
      'step',
      ['zoom'],
      0,
      10,
      0.31,
      15,
      0.62,
    ]);
    expect(jsonEncode(step), before);
    expect(
      mapboxWeightedTextOpacity([
        'interpolate',
        ['linear'],
        ['zoom'],
        8,
        0,
        14,
        1,
      ], 0.56),
      [
        'interpolate',
        ['linear'],
        ['zoom'],
        8,
        0,
        14,
        0.56,
      ],
    );
    expect(
      mapboxWeightedTextOpacity([
        'interpolate',
        ['exponential', 1.5],
        ['zoom'],
        8,
        0.2,
        14,
        1,
      ], 0.5),
      [
        'interpolate',
        ['exponential', 1.5],
        ['zoom'],
        8,
        0.1,
        14,
        0.5,
      ],
    );
  });

  test('unsupported or malformed expressions are skipped without fallback', () {
    for (final value in _unsupported) {
      expect(mapboxWeightedTextOpacity(value, 0.62), isNull, reason: '$value');
    }
  });

  test(
    'real GL bridge matches Dart policy and resets only for a new style',
    () async {
      final vectors = <Map<String, Object?>>[
        for (final original in [
          null,
          0,
          0.8,
          [
            'step',
            ['zoom'],
            0,
            10,
            0.5,
            15,
            1,
          ],
          [
            'interpolate',
            ['linear'],
            ['zoom'],
            8,
            0,
            14,
            1,
          ],
          [
            'interpolate',
            ['exponential', 1.5],
            ['zoom'],
            8,
            0.2,
            14,
            1,
          ],
          ..._unsupported,
        ])
          _vector('road-minor-label', original: original),
        _vector('settlement-subdivision-label', original: 0.5),
        _vector('custom', source: 'neighborhood_label'),
        for (final id in [
          'road-label',
          'road-primary-label',
          'road-motorway-shield',
          'settlement-major-label',
          'settlement-minor-label',
          'country-label',
          'state-label',
          'water-label',
          'point-annotations',
          'unknown-label',
        ])
          _vector(id),
        _vector('district-label', icon: 'shield'),
        _vector('district-label', source: 'water'),
        _vector('custom', source: 'place_label'),
      ];
      final process = await Process.run(
        Platform.environment['BIRDTIE_TEST_NODE'] ?? 'node',
        [
          '-e',
          _bridgeHarness,
          File('web/birdtie_mapbox.js').absolute.path,
          jsonEncode({'policy': mapboxLabelWeightPolicy, 'vectors': vectors}),
        ],
      );
      expect(
        process.exitCode,
        0,
        reason: '${process.stderr}\n${process.stdout}',
      );
      expect(process.stdout.toString().trim(), 'LABEL_WEIGHT_PASS');
    },
  );
}

const _unsupported = <Object>[
  ['get', 'opacity'],
  [
    '*',
    0.5,
    [
      'interpolate',
      ['linear'],
      ['zoom'],
      8,
      0,
      14,
      1,
    ],
  ],
  [
    'step',
    ['zoom'],
    1,
    10,
  ],
  [
    'step',
    ['zoom'],
    0,
    10,
    0.5,
    10,
    1,
  ],
  [
    'step',
    ['zoom'],
    0,
    10,
    2,
  ],
  [
    'step',
    ['get', 'rank'],
    0,
    10,
    1,
  ],
  [
    'interpolate',
    ['linear'],
    ['zoom'],
    10,
    1,
  ],
  [
    'interpolate',
    ['linear'],
    ['zoom'],
    14,
    0,
    8,
    1,
  ],
  [
    'interpolate',
    ['exponential', 0],
    ['zoom'],
    8,
    0,
    14,
    1,
  ],
  [
    'interpolate',
    ['linear'],
    ['zoom'],
    8,
    0,
    14,
    ['get', 'opacity'],
  ],
  [
    'let',
    'value',
    1,
    [
      'step',
      ['zoom'],
      0,
      10,
      1,
    ],
  ],
];

Map<String, Object?> _vector(
  String id, {
  String source = '',
  String? icon,
  Object? original,
}) {
  final factor = mapboxSecondaryLabelFactor(
    id,
    'symbol',
    sourceLayer: source,
    hasIconImage: icon != null,
  );
  return {
    'id': id,
    'source': source,
    'icon': icon,
    'original': original,
    'factor': factor,
    'expected': factor == null
        ? null
        : mapboxWeightedTextOpacity(original, factor),
  };
}

const _bridgeHarness = r'''
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const input=JSON.parse(process.argv[2]),created=[];
class Map {
  constructor(options){this.listeners={};this.layers=[];this.writes=[];created.push(this);}
  on(kind,fn){this.listeners[kind]=fn;} jumpTo(){} resize(){} remove(){this.removed=true;}
  getStyle(){return {layers:this.layers};}
  setLayoutProperty(){}
  setPaintProperty(id,key,value){
    if(id==='district-label'&&this.failPaint)throw Error('synthetic failure');
    this.writes.push({id,key,value});
    this.layers.find(l=>l.id===id).paint[key]=value;
  }
}
const element={isConnected:true,clientWidth:430,clientHeight:900,style:{}};
const window={mapboxgl:{Map,supported:()=>true}},document={
  createElement:()=>({remove(){}}),getElementById:()=>element,
  head:{appendChild:el=>queueMicrotask(()=>el.onload())}};
class ResizeObserver{observe(){}disconnect(){}}
vm.runInNewContext(fs.readFileSync(process.argv[1],'utf8'),
  {window,document,ResizeObserver,setTimeout,clearTimeout,performance});
const plain=v=>JSON.parse(JSON.stringify(v));
const config=policy=>JSON.stringify({accessToken:'pk.synthetic',style:'mapbox://styles/synthetic',
  camera:{center:[-2,57],zoom:12,bearing:0,pitch:0},labelWeightPolicy:policy});
const layer=(v,i)=>({id:v.id,type:'symbol','source-layer':v.source,
  layout:{'icon-image':v.icon},paint:{'text-opacity':plain(v.original)}});
(async()=>{
  await window.birdtieMapboxMount('map',config(input.policy),()=>{});
  const map=created[0];
  for(const [i,v] of input.vectors.entries()){
    map.layers=[layer(v,i)];map.writes=[];map.listeners['style.load']();map.listeners.load();
    if(v.expected==null){assert.equal(map.writes.length,0,JSON.stringify(v));}
    else{
      assert.equal(map.writes.length,2,JSON.stringify(v));
      assert.deepEqual(plain(map.writes[0].value),v.expected);
      assert.deepEqual(plain(map.writes[1].value),v.expected,'second load must not multiply again');
    }
    const m=JSON.parse(window.birdtieMapboxStyleMetrics('map'));
    assert.equal(m.matched,v.factor==null?0:1);
    assert.equal(m.applied,v.expected==null?0:1);
    assert.equal(m.skipped,v.factor!=null&&v.expected==null?1:0);
  }
  map.layers=[{id:'district-label',type:'symbol',paint:{'text-opacity':0.8}}];
  map.listeners['style.load']();
  assert.equal(map.layers[0].paint['text-opacity'],0.8*0.56,'real new style uses new original');
  map.layers=[{id:'district-label',type:'symbol',paint:{'text-opacity':1}}];map.failPaint=true;
  map.listeners['style.load']();
  let metrics=JSON.parse(window.birdtieMapboxStyleMetrics('map'));
  assert.equal(metrics.applied,0);assert.equal(metrics.skipped,1);
  window.birdtieMapboxDestroy('map');map.writes=[];map.listeners['style.load']();
  assert.equal(map.writes.length,0);assert.equal(window.birdtieMapboxStyleMetrics('map'),null);
  await window.birdtieMapboxMount('missing',config(null),()=>{});
  const missing=created[1];missing.layers=[{id:'district-label',type:'symbol',paint:{}}];
  missing.listeners['style.load']();assert.equal(missing.writes.length,0);
  window.birdtieMapboxDestroy('missing');console.log('LABEL_WEIGHT_PASS');
})().catch(e=>{console.error(e.stack);process.exitCode=1});
''';
