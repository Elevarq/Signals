# Listing assets

`architecture.svg` / `architecture.png` are **generated** from the Mermaid
source in [`docs/architecture.md`](../../architecture.md) — that file is the
single source of truth. Regenerate after editing the diagram:

```sh
# extract the mermaid block and render (SVG for the repo, PNG for the upload)
awk '/^```mermaid$/{f=1;next} /^```$/{if(f)exit} f' docs/architecture.md > /tmp/arch.mmd
npx -y @mermaid-js/mermaid-cli -i /tmp/arch.mmd -o docs/marketplace/assets/architecture.svg -b white
npx -y @mermaid-js/mermaid-cli -i /tmp/arch.mmd -o docs/marketplace/assets/architecture.png -b white -s 2
```

Use `architecture.png` for the AWS Marketplace product architecture-diagram
upload.

> **Draft quality.** This is the default Mermaid render — accurate but wide and
> unstyled. It is fine as a working/review asset, but for the **public listing
> image** a cleaner, narrower, branded diagram is recommended before going
> Public.
