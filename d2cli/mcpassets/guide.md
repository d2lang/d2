# D2 guide

D2 turns text into diagrams. It is built for software diagrams: architecture, sequence, entity-relationship and class diagrams. It is not for charts, Gantt charts or mind maps.

## Workflow

1. Write the D2 source.
2. Call `render` and look at the image.
3. If it failed, fix the reported lines and render again. If it rendered, check that nothing overlaps, that the text is readable, and that the flow reads in the direction you intended.

For animated diagrams, call `storyboard` to see the boards in playback order, with a timestamp under each frame. Call `lint_motion` to find changes that are hard to follow. Edit the reported step and review the new storyboard.

## House style

Follow these five rules and most mistakes cannot happen.

1. Names are plain identifiers: letters, digits, `_` and `-`. Put the visible text in a quoted label.
2. Quote every label that contains punctuation.
3. Declare a container with braces before you connect to something inside it.
4. From the top level, refer to a nested shape by its full path, such as `cluster.web`.
5. Keep one board under about 25 shapes. Use layers for detail.

```d2
direction: right

user: "Shopper" {shape: person}

edge: "Edge" {
  cdn: "CDN"
  gateway: "API gateway" {shape: hexagon}
  cdn -> gateway: "cache miss"
}

cluster: "Kubernetes cluster" {
  web: "Web BFF"
  orders: "Orders"
  web -> orders: "place order"
}

data: "Data plane" {
  orders_db: "Orders DB" {shape: cylinder}
  events: "Kafka" {shape: queue}
}

user -> edge.cdn: "HTTPS"
edge.gateway -> cluster.web: "REST"
cluster.orders -> data.orders_db: "write"
cluster.orders -> data.events: "OrderPlaced"
```

## Basics

```d2
# A shape. The name is also the label unless you give one.
api
db: "Orders DB"

# Connections. The label goes after the colon.
api -> db: "reads"
api <- db: "rows"
api <-> db
api -- db

# A chain
a -> b -> c
```

Writing the same connection twice makes two connections.

## Shapes

Set `shape` to change what a node looks like. The ones used most in software diagrams:

```d2
db: "Orders DB" {shape: cylinder}
topic: "order-events" {shape: queue}
user: "Customer" {shape: person}
stripe: "Stripe" {shape: cloud}
gateway: "API gateway" {shape: hexagon}
bucket: "Invoices bucket" {shape: stored_data}
decision: "In stock?" {shape: diamond}
```

There is no `database` shape: use `cylinder`. Call `guide` with topic `reference` for the full list.

## Containers

```d2
client: "Client"
vpc: "VPC" {
  app: "App server"
  cache: "Redis" {shape: cylinder}
  app -> cache
  app -> _.client: "push"
}
client -> vpc.app
```

Inside a container, `_` is the parent.

## Styles

Style keys go under `style`. Colors can be CSS names or hex codes, and `fill` and `stroke` also accept gradients. Hex codes such as `#ff0000` work with or without quotes.

```d2
api: "API" {
  style: {
    fill: "#EEF4FF"
    stroke: "#3B6FD9"
    stroke-width: 2
    border-radius: 6
    font-color: navy
    shadow: true
  }
}
db: "DB" {shape: cylinder}
api -> db: "async" {
  style.stroke-dash: 4
  style.animated: true
}
```

On a connection, `icon.style.animated: 2/s` launches two icons each second.
Set `icon.style.animated: 200ms` to loop one icon with a fixed trip time of
200 milliseconds, or use `true` to loop it at the default travel speed.
A step can set the rate to `0/s`
to let the stream drain. The default icon is a filled circle in the wire's
color. Set `icon` to an image path to use your own artwork.
Choose what one icon represents for your diagram, for example a batch of 100
requests. D2 caps the displayed rate so a steady stream has at most 64 icons in flight.
On a wire with a two-second trip, `32/s` and higher rates all show 32
departures per second.

For a single-board GIF, omit `animate-interval` to let D2 derive the loop
from the animation. A stream at `0.25/s` needs a four-second loop. If you
specify an interval, the full exported loop must cover at least one departure
period for each positive rate and one full trip for each fixed duration.
Individual steps can be shorter. If several
streams have no common loop within the GIF frame limit, choose an explicit
duration that covers the slowest stream. SVG can play continuously when even
one departure period exceeds the GIF limit.

```d2
relay -> broker: OrderPlaced {
  icon: ./envelope.svg
  icon.style.animated: 2/s
}
```

Reuse styles with classes:

```d2
classes: {
  service: {
    style.fill: "#EEF4FF"
    style.stroke: "#3B6FD9"
  }
  store: {
    shape: cylinder
    style.fill: "#FFF4E5"
  }
}
orders: "Orders" {class: service}
orders_db: "Orders DB" {class: store}
orders -> orders_db
```

To restyle a connection that already exists, refer to it by index:

```d2
a -> b
(a -> b)[0].style.stroke: red
```

## Layout

`direction` at the top sets the flow: `right`, `down`, `left` or `up`.

Choose the layout engine in the `render` call, or in the source:

```d2
vars: {
  d2-config: {
    layout-engine: tala
  }
}
a -> b
```

Use `tala` for architecture diagrams with containers. Use `dagre` or `elk` for simple flows.

A title:

```d2
title: "Checkout: runtime view" {
  shape: text
  near: top-center
  style.font-size: 28
}
a -> b
```

A grid:

```d2
regions: "Regions" {
  grid-columns: 3
  us: "us-east-1"
  eu: "eu-west-1"
  ap: "ap-south-1"
}
```

## Sequence diagrams

The order of the lines is the order of the messages. Declare the actors first to fix their order from left to right.

```d2
shape: sequence_diagram
client: "Web client"
api: "API"
db: "Orders DB"

client -> api: "POST /orders"
api -> db: "insert"
db -> api: "ok"
api -> client: "201 Created"
```

An activation span is a name under the actor. A group is a container holding messages.

```d2
shape: sequence_diagram
client: "Web client"
api: "API"
db: "Orders DB"

client -> api.handle: "POST /orders"
api.handle -> db: "insert"
db -> api.handle: "ok"

retry: "if the insert fails" {
  api.handle -> db: "insert again"
}
api.handle -> client: "201 Created"
```

## Tables and classes

```d2
orders: {
  shape: sql_table
  id: int {constraint: primary_key}
  customer_id: int {constraint: foreign_key}
  total: decimal
}
customers: {
  shape: sql_table
  id: int {constraint: primary_key}
  email: varchar
}
orders.customer_id -> customers.id
```

```d2
OrderService: {
  shape: class
  +repo: OrderRepository
  +place(cart Cart): Order
  -validate(cart Cart): bool
}
```

## Boards

Layers hold detail that you open from the main board. Render one with `target`, for example `layers.api`.

```d2
web -> api -> db
api.link: layers.api

layers: {
  api: {
    handler -> service -> repository
  }
}
```

`scenarios` are variations of the board they sit in. `steps` build on one another in order.

```d2
a -> b
scenarios: {
  failover: {
    b.style.opacity: 0.3
    a -> c
  }
}
```

## Icons, links and text

An icon is a URL. Icons for common technology are hosted at https://icons.d2lang.com. If you do not know the exact URL, leave the icon out: a wrong URL renders a broken image.

```d2
db: "Postgres" {
  shape: cylinder
  icon: https://icons.d2lang.com/dev%2Fpostgresql.svg
}
```

```d2
api: "API" {
  tooltip: "Owned by the platform team"
  link: https://example.com/runbook
}
```

Longer text is a Markdown block:

```d2
notes: |md
  ## Deploy order
  1. database
  2. services
|
```

## Mistakes to avoid

| You wrote | What happens | Write this |
|---|---|---|
| `main.go -> utils.go` | A dot nests. You get containers `main` and `utils`, each holding `go`. | `"main.go" -> "utils.go"` |
| `api.example.com`, `10.0.0.1`, `v1.2` as names | Each dot starts a nested shape. | `"api.example.com"`, `"10.0.0.1"`, `"v1.2"`. |
| `nginx:1.25 -> redis:7` | The first colon starts the label of a single shape named `nginx`. | `"nginx:1.25" -> "redis:7"` |
| `a -> b: GET /users/{id}` | `{` starts a map. | `a -> b: "GET /users/{id}"` |
| `a: C# service` | The hash after `C` starts a comment, so the label is `C`. A leading hash stays literal, as in `a: #tag`. | `a: "C# service"` |
| `a: "${price}"` for literal text | `${price}` expands a variable. A plain `$`, as in `"costs $5"`, stays literal. | `a: '${price}'` |
| `a: read; write` | `;` separates statements. You get two shapes. | `a: "read; write"` |
| `Repo<Order>` as a shape name | `<` starts an edge. In a label, `a: Repo<Order>`, `a: string[]`, and `a: R&D` all work. | `"Repo<Order>"` |
| `direction: LR` | Not valid. | `direction: right` |
| `shape: database` | Not a shape. | `shape: cylinder` |
| `fill: red` inside a shape | Style keys need the prefix. | `style.fill: red` |
| `color: white` inside a shape | Creates a child shape named `color`. | `style.font-color: white` |
| `// note` | Creates a shape named `// note`. | `# note` |
| `subgraph x {}` | Creates a shape named `subgraph x`. | `x: {}` |
| `layers`, `steps` or `scenarios` as a shape at the root of a board | Those keys declare boards. Inside another shape, they name ordinary shapes. | Quote the name at the board root, for example `"layers"`. |
| `a -> b` again to change its style | Makes a second connection. | `(a -> b)[0].style.stroke: red` |
| `x.class: servce` (typo) | No error. The shape is unstyled. | Use the declared class name, for example `x.class: service`. |

Reserved words cannot be shape names unless quoted: `label`, `shape`, `icon`, `style`, `class`, `classes`, `vars`, `link`, `near`, `tooltip`, `width`, `height`, `top`, `left`, `direction`, `constraint`, and the style words such as `fill`, `stroke`, `opacity`, `font`, `bold`, `shadow`.
