# Geometry staging

`stageGeometry`는 document가 동일한 layout commit을 게시하기 전에 선언된 surface rectangle의 유한한
집합을 적용합니다. Controller는 전체 inventory에 대한 compositor receipt를 반환합니다. 호출자는 receipt가
성공한 뒤에만 DOM layout을 게시합니다.

Controller는 관련 없는 mutation과 resize notification이 실행되는 동안 staged rectangle을 유지합니다.
지정된 모든 declaration이 동일한 rectangle을 보고하면 geometry lease를 제거합니다. 이후 DOM 변경은 다시
측정된 rectangle을 사용합니다.

Command는 선언되지 않은 surface id와 음수 또는 유한하지 않은 값이 포함된 rectangle을 거부합니다.
Provider 내부 구현을 검사하지 않으며 surface 관계를 추론하지 않습니다.
