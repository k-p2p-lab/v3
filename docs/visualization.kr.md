# 실험 결과 이미지

[English](visualization.md) | 한국어

**Saved results → Images**를 누르면 해당 실험의 그래프 이미지를 작은 창에서 볼 수 있습니다. 이미지 또는 **PNG ↓**를 누르면 그 이미지가 다운로드됩니다. 흰 배경의 PNG는 가로 1,600픽셀이며 제목, 실험 식별자, snapshot 시각, 축과 범례를 포함합니다.

전달 지연 CDF·히스토그램과 메시지 활동 이미지를 생성합니다. 기록이 있으면 GossipSub 제어 트래픽, 전체 P2P 스트림 대역폭, 그룹별 Peer 스코어와 GossipSub mesh 평균 차수 이미지도 표시합니다. 그룹은 한 이미지의 여러 선으로 표시합니다. 자료가 없는 과거 결과에 토폴로지나 스코어를 만들어 넣지 않습니다.

실행 중인 결과는 버튼을 누른 시점의 snapshot입니다. 창을 닫으면 진행 중인 요청을 취소하며, 다시 열면 새 snapshot으로 이미지를 생성합니다. 오류가 나면 **Retry**로 다시 시도할 수 있습니다. 여러 실행 선택, 기준 실행 비교, 필터, JSON/CSV/SVG 내보내기 화면은 제거했습니다.

## 데이터와 의미

- 지연은 기존 v3 집계기의 유효 첫 수신 표본을 사용합니다. CDF는 지연 표본의 누적 비율이며 전체 Peer 도달률이 아닙니다. 정의와 delivery window를 이미지에 기록합니다.
- 메시지 활동은 저장된 publish/deliver/duplicate 이벤트 수를 구간 초로 나눈 값입니다. local·late 수신도 포함하므로 전달률로 읽지 않습니다.
- 제어 트래픽은 control type별 send/recv/drop RPC 관측 횟수입니다. 여러 control type이 한 RPC에 포함될 수 있으며 send와 recv는 양쪽 관측을 따로 셉니다.
- 대역폭은 전체 libp2p 스트림의 kbit/s입니다. IP/TCP framing·재전송·관리 API 트래픽은 제외합니다.
- 스코어와 mesh 차수는 기존 `observations.jsonl`을 사용합니다. 스코어는 관측자별 평가의 평균이고 차수는 신선한 보고에 나타난 고유 이웃 수입니다. 결측은 0으로 채우지 않습니다.

기존 `GET /api/v1/experiments/{id}/analysis`로 저장 파일을 읽고 브라우저에서 PNG를 생성합니다. Python이나 별도 이미지 서버를 추가하지 않습니다. 전체 로그의 지표 집계, 관측 수집, 원본 결과 ZIP의 형식은 유지합니다. 요청은 자동으로 반복하지 않습니다.

[실험 지표](experiment-metrics.kr.md) · [모니터링과 결과 보존](monitoring.kr.md)
