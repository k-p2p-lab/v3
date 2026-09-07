# Input: a Controller /api/v1/snapshot response; argument: --arg run RUN_ID.
# Scores are directed observer -> evaluated peer observations, not self-scores.
if $run == "" then error("run ID is required") else . end
| . as $snapshot
| INDEX(.agents[] | select(.state == "online"); .id) as $online
| [.nodes[] | select(.runId == $run)] as $nodes
| INDEX($nodes[]
    | select(.peerId != null and .peerId != "")
    | select(.state == "ready" and $online[.agentId] != null); .peerId) as $ready
| [$ready[] as $observer
    | ($observer.peerScores // {} | to_entries[]) as $entry
    | $ready[$entry.key] as $subject
    | select($subject != null)
    # Exclude retained scores for peers without a current transport connection.
    | select((($observer.connectedPeers // []) | index($entry.key)) != null)
    | {observerGroup: $observer.group, evaluatedGroup: $subject.group,
       score: $entry.value,
       inMesh: ((($observer.meshPeers["kpl/prysm/beacon_block"] // []) | index($entry.key)) != null)}]
| group_by([.observerGroup, .evaluatedGroup])
| map({observerGroup: .[0].observerGroup, evaluatedGroup: .[0].evaluatedGroup,
       pairs: length,
       meanScore: (map(.score) | add / length),
       minScore: (map(.score) | min), maxScore: (map(.score) | max),
       negativePairs: (map(select(.score < 0)) | length),
       meshPairs: (map(select(.inMesh)) | length)}) as $scores
| {generatedAt: $snapshot.generatedAt, runId: $run,
   populations: ($nodes | group_by(.group) | map({
     group: .[0].group,
     ready: (map(select(.state == "ready" and $online[.agentId] != null)) | length),
     starting: (map(select(.state == "starting" and $online[.agentId] != null)) | length)
   })),
   scores: $scores}
