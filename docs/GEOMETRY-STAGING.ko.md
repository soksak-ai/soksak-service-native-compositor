# Geometry staging

`stageGeometry`는 document가 동일한 layout commit을 게시하기 전에 선언된 surface rectangle의 유한한
집합을 적용합니다. Controller는 전체 inventory에 대한 compositor receipt를 반환합니다. 호출자는 receipt가
성공한 뒤에만 DOM layout을 게시합니다.

Controller는 관련 없는 mutation과 resize notification이 실행되는 동안 staged rectangle을 유지합니다.
호출자는 DOM layout을 게시한 뒤 `releaseGeometry`로 lease를 끝냅니다. Controller는 그때 측정된 rectangle을
commit하고, 이후 DOM 변경은 다시 측정된 rectangle을 사용합니다.

Lease는 측정으로 끝나지 않습니다. Staged rectangle은 document가 아직 만들지 않은 layout에 대한 호출자의
예측이고, surface를 선언하는 element는 그 소유자가 배치합니다 — 터미널 플러그인은 element를 정수 px로
배치하고, 그 element가 채우는 pane은 소수 px입니다. 2026-09-05 실측: 폭 160.26으로 staged 된 rectangle을
document는 160으로 배치했고, 둘이 같아지기를 기다리던 lease는 세션이 끝날 때까지 surface를 staged
rectangle에 붙잡아 이후의 모든 layout 변경이 document만 옮기고 surface는 옮기지 못했습니다.

Command는 선언되지 않은 surface id와 음수 또는 유한하지 않은 값이 포함된 rectangle을 거부합니다.
Provider 내부 구현을 검사하지 않으며 surface 관계를 추론하지 않습니다.
