export default [
  {
    id: "merge-edges",
    title: "Merge edges",
    group: "Connections",
    key: "elk.layered.mergeEdges",
    values: [false, true],
    source:
      'direction: right\n\nGateway -> Orders\nGateway -> Billing\nGateway -> Events\n\nGateway.style.fill: "#f7d6bb"\n',
    description: "Let outgoing connections share an attachment point before they branch.",
    insight:
      "Follow the three lines leaving Gateway. With merging enabled they leave from the center of its right edge. Each arrow still reaches its own destination.",
    example: "Gateway fan-out",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-layered-mergeEdges.html",
    option: "elk-mergeEdges",
  },
  {
    id: "edge-routing",
    title: "Edge routing",
    group: "Connections",
    key: "elk.edgeRouting",
    values: ["ORTHOGONAL", "POLYLINE"],
    labels: ["Orthogonal", "Polyline"],
    source:
      'direction: right\n\nGateway -> Orders\nGateway -> Billing\nGateway -> Events\n\nGateway.style.fill: "#f7d6bb"\n',
    description: "Choose how ELK draws the segments between connected shapes.",
    insight:
      "Orthogonal routing turns at right angles. With polyline routing, the outer branches run diagonally between Gateway and its destinations.",
    example: "Gateway fan-out",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-edgeRouting.html",
    option: "elk-edgeRouting",
  },
  {
    id: "feedback-edges",
    title: "Feedback edges",
    group: "Connections",
    key: "elk.layered.feedbackEdges",
    values: [false, true],
    source:
      'direction: right\n\nRead -> Parse -> Write -> Done\nWrite -> Read\nDone -> Parse\n\nWrite.style.fill: "#f7d6bb"\n',
    description: "Give connections that travel against the main flow their own routes.",
    insight:
      "Trace Write → Read and Done → Parse. With feedback routing enabled, the return paths leave the far side of their sources and run around the main flow.",
    example: "Retry cycle",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-layered-feedbackEdges.html",
    option: "elk-feedbackEdges",
  },
  {
    id: "node-spacing",
    title: "Node spacing",
    group: "Spacing",
    key: "elk.spacing.nodeNode",
    values: [12, 80, 140],
    source:
      'direction: right\n\nGateway -> Orders\nGateway -> Billing\nGateway -> Events\n\nGateway.style.fill: "#f7d6bb"\n',
    description: "Set the minimum gap between neighboring nodes in the same layer.",
    insight:
      "Orders, Billing and Events share a layer. Raising this setting opens the vertical space between them while Gateway stays in the preceding layer.",
    example: "Gateway fan-out",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-spacing-nodeNode.html",
    option: "elk-nodeNode",
  },
  {
    id: "edge-node-spacing",
    title: "Edge–node clearance",
    group: "Spacing",
    key: "elk.spacing.edgeNode",
    values: [8, 80, 140],
    source:
      'direction: right\n\nInput -> Decode -> Check -> Save\nInput -> Parse -> Audit -> Save\nInput -> Check\nInput -> Audit\nDecode -> Audit\nParse -> Save\n\nDecode.style.fill: "#f7d6bb"\n',
    description: "Give passing connections clearance from nodes in a layer.",
    insight:
      "Follow the long connections that skip a step. As clearance grows, ELK gives those lines more room beside the intervening nodes.",
    example: "Processing pipeline",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-spacing-edgeNode.html",
    option: "elk-edgeNode",
  },
  {
    id: "edge-spacing",
    title: "Edge spacing",
    group: "Spacing",
    key: "elk.spacing.edgeEdge",
    values: [6, 64, 100],
    source:
      'direction: right\n\nInput -> Read -> Test -> Ship\nInput -> Build -> Log\nBuild -> Ship\nInput -> Ship\n\nInput.style.fill: "#f7d6bb"\n',
    description: "Separate neighboring connections as they pass through a layer.",
    insight:
      "Watch the parallel runs of the long connections in the middle of the diagram. Routing can move the nodes as well to make room.",
    example: "Processing pipeline",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-spacing-edgeEdge.html",
    option: "elk-edgeEdge",
  },
  {
    id: "layer-node-spacing",
    title: "Nodes between layers",
    group: "Spacing",
    key: "elk.layered.spacing.nodeNodeBetweenLayers",
    values: [24, 130, 220],
    source: 'direction: right\n\nRead -> Build -> Ship\n\nBuild.style.fill: "#f7d6bb"\n',
    description: "Set the minimum gap between nodes in successive layers.",
    insight:
      "Read, Build and Ship occupy three consecutive layers. Raising the spacing lengthens both horizontal runs.",
    example: "Three-stage build",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-layered-spacing-nodeNodeBetweenLayers.html",
    option: "elk-nodeNodeBetweenLayers",
  },
  {
    id: "layer-edge-node-spacing",
    title: "Edges beside layers",
    group: "Spacing",
    key: "elk.layered.spacing.edgeNodeBetweenLayers",
    values: [8, 88, 140],
    source:
      'direction: right\n\nQueue -> Store\nBatch -> Store\n\nStore.style.fill: "#f7d6bb"\n',
    description:
      "Set the clearance between a node layer and a connection routed alongside it.",
    insight:
      "The bend joining these two producers to Store needs room between the layers. Increasing clearance pushes the layers apart.",
    example: "Shared destination",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-layered-spacing-edgeNodeBetweenLayers.html",
    option: "elk-edgeNodeBetweenLayers",
  },
  {
    id: "layer-edge-spacing",
    title: "Edges between layers",
    group: "Spacing",
    key: "elk.layered.spacing.edgeEdgeBetweenLayers",
    values: [5, 65, 110],
    source:
      'direction: right\n\nAPI -> East\nAPI -> West\nWeb -> East\nWeb -> West\nJob -> East\nJob -> West\n\nWeb.style.fill: "#f7d6bb"\n',
    description:
      "Set the gap between parallel edge segments in the channel between two layers.",
    insight:
      "Three producers connect to both destinations. Increase the setting to spread the vertical segments across a wider channel.",
    example: "Shared destinations",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-layered-spacing-edgeEdgeBetweenLayers.html",
    option: "elk-edgeEdgeBetweenLayers",
  },
  {
    id: "container-padding",
    title: "Container padding",
    group: "Spacing",
    key: "elk.padding",
    values: [
      "[top=12,left=12,bottom=12,right=12]",
      "[top=65,left=65,bottom=65,right=65]",
      "[top=100,left=100,bottom=100,right=100]",
    ],
    labels: ["12 px on all sides", "65 px on all sides", "100 px on all sides"],
    source:
      'direction: right\n\nWorker: {\n  Read -> Write\n}\nWorker.style.stroke: "#2c574d"\n',
    description: "Change the inset between a container’s border and its children.",
    insight:
      "Compare the space around Read and Write inside Worker. D2 also reserves room for the container label, so a small requested inset can grow on the label side.",
    example: "Worker container",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-padding.html",
    option: "elk-padding",
  },
  {
    id: "self-loop-spacing",
    title: "Self-loop spacing",
    group: "Spacing",
    key: "elk.spacing.nodeSelfLoop",
    values: [20, 90, 150],
    source:
      'direction: right\n\nWorker -> Worker: retry\n\nWorker.style.fill: "#f7d6bb"\n',
    description: "Change how far a self-loop travels away from its node.",
    insight:
      "Follow the retry arrow around Worker. More spacing gives the loop a larger footprint while the node keeps its size.",
    example: "Worker retry",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-spacing-nodeSelfLoop.html",
    option: "elk-nodeSelfLoop",
  },
  {
    id: "wrapping",
    title: "Pipeline wrapping",
    group: "Order & shape",
    key: "elk.layered.wrapping.strategy",
    values: ["OFF", "SINGLE_EDGE", "MULTI_EDGE"],
    labels: ["Off", "Single edge", "Multi edge"],
    source:
      'direction: right\n\nGet -> Read -> Parse -> Check -> Build -> Test -> Pack -> Sign -> Ship\n\nGet.style.fill: "#f7d6bb"\n',
    description: "Fold a long chain of stages into several rows.",
    insight:
      "Follow the arrows from Get to Ship. Wrapping trades width for height. Single-edge and multi-edge wrapping can choose the same cuts on a simple chain.",
    example: "Release pipeline",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-layered-wrapping-strategy.html",
    option: "elk-wrappingStrategy",
  },
  {
    id: "model-order",
    title: "Declaration order",
    group: "Order & shape",
    key: "elk.layered.crossingMinimization.forceNodeModelOrder",
    values: [false, true],
    source:
      'direction: right\n\nStart\nAlpha\nBeta\nOne\nTwo\nEnd\n\nStart -> Alpha\nStart -> Beta\nAlpha -> Two\nBeta -> One\nOne -> End\nTwo -> End\n\nAlpha.style.fill: "#f7d6bb"\n',
    description: "Keep peers in D2 declaration order when ELK minimizes crossings.",
    insight:
      "The source declares One before Two but connects Alpha to Two and Beta to One. Preserving both layer orders forces a crossing. This example sets the option at the diagram root.",
    example: "Crossed dependencies",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-layered-crossingMinimization-forceNodeModelOrder.html",
    option: "elk-forceNodeModelOrder",
  },
  {
    id: "layer-constraint",
    title: "First or last layer",
    group: "Order & shape",
    key: "elk.layered.layering.layerConstraint",
    values: ["NONE", "LAST", "FIRST"],
    labels: ["Unconstrained", "Last", "First"],
    source:
      'direction: right\n\nStart -> Read -> Build -> Test -> Ship\nStart -> Audit\n\nAudit.style.fill: "#f7d6bb"\n',
    description: "Anchor one named shape to the first or last layer.",
    insight:
      "Watch the highlighted Audit node. Its only connection is from Start. A constraint on Audit changes where that branch sits relative to the main pipeline.",
    target: "Audit",
    example: "Audit branch",
    docs: "https://eclipse.dev/elk/reference/options/org-eclipse-elk-layered-layering-layerConstraint.html",
    option: "elk-layer-constraint",
  },
];
