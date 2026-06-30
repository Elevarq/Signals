# EULA review notes — Elevarq Signals (AWS Marketplace)

Internal notes on the AWS Marketplace EULA decision. **Not** part of the
customer-facing contract — the upload-ready EULA is [`EULA.md`](EULA.md), which
must contain clean customer-facing text only. This file holds the rationale and
the open counsel gates.

> This is not legal advice. The items under "Counsel gates" must be confirmed
> with counsel before the listing is submitted. Signals is GA at v1.0.0; the
> listing — and therefore this EULA — is gated on counsel's legal sign-off.

## Custom EULA vs Standard Contract (SCMP)

AWS Marketplace container products may use either the **Standard Contract for
AWS Marketplace (SCMP)** or a **custom EULA**. A custom EULA is attached to a
free offer as a `CustomEula` legal-term document — a PDF in an accessible S3
bucket that the Catalog API references by S3 URL — so the uploaded text must be
a clean, customer-facing contract with no draft markers or internal notes.

**Decision:** use the **custom EULA in [`EULA.md`](EULA.md)**, which grants use
under the product's existing **BSD-3-Clause** license. Rationale: Signals is
already distributed under that permissive license, and the SCMP's
commercial-support / warranty framing is aimed at paid products. The SCMP
remains a fallback if counsel prefers a standardized contract.

When 1.0 ships, render `EULA.md` to PDF and host it at
`https://elevarq-marketplace-public.s3.amazonaws.com/eula/elevarq-signals-eula-v1.pdf`;
the free offer's legal term then references that S3 URL
(`{Type: LegalTerm, Documents: [{Type: CustomEula, Url: <s3-pdf>}]}`).

## Counsel gates (confirm before submit)

1. **Legal entity / party name.** The EULA names **Scantr LLC, doing business
   as Elevarq**. Confirm this is the correct contracting party and matches the
   AWS Marketplace **seller registration** record.
2. **LICENSE copyright alignment.** The repository `LICENSE` (BSD-3-Clause)
   copyright line currently reads **"Elevarq"** (`Copyright (c) 2026,
   Elevarq`). Decide whether it should read **"Scantr LLC dba Elevarq"** so the
   seller entity, the EULA party, and the `LICENSE` copyright holder are
   intentionally aligned. (If the line is changed, do it as its own change with
   counsel sign-off.)
3. **Free-listing framing.** Confirm the "no software fee" wording (Section 2)
   and the absence of any support upsell satisfy AWS's container-product
   policies for free products (no metadata redirecting buyers to offerings not
   available on AWS Marketplace).

## Customer-facing hygiene

The uploaded [`EULA.md`](EULA.md) must not contain internal-only terms
("Draft", "counsel", "recommended option", "TODO", etc.). Re-check before each
submission.
