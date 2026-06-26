# Listing assets

Two diagrams, both rendered from Mermaid sources:

| File | Source | Use |
|------|--------|-----|
| `architecture-listing.{svg,png}` | `architecture-listing.mmd` (this dir) | **The AWS Marketplace listing image** — simplified, branded, value-focused. Upload `architecture-listing.png`. |
| `architecture.{svg,png}` | the Mermaid block in [`docs/architecture.md`](../../architecture.md) | The full engineering component / data-flow diagram (reference). |

Regenerate after editing either source:

```sh
# Listing diagram
npx -y @mermaid-js/mermaid-cli -i docs/marketplace/assets/architecture-listing.mmd \
  -o docs/marketplace/assets/architecture-listing.svg -b white
npx -y @mermaid-js/mermaid-cli -i docs/marketplace/assets/architecture-listing.mmd \
  -o docs/marketplace/assets/architecture-listing.png -b white -s 3

# Full engineering diagram (architecture.md is the single source of truth)
awk '/^```mermaid$/{f=1;next} /^```$/{if(f)exit} f' docs/architecture.md > /tmp/arch.mmd
npx -y @mermaid-js/mermaid-cli -i /tmp/arch.mmd -o docs/marketplace/assets/architecture.svg -b white
npx -y @mermaid-js/mermaid-cli -i /tmp/arch.mmd -o docs/marketplace/assets/architecture.png -b white -s 2
```

> The listing diagram is a clean Mermaid render. If a designer later produces a
> hand-built branded image, drop it in here and point the listing at it.
