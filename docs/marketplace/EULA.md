# Elevarq Signals — End User License Agreement

This End User License Agreement ("Agreement") governs your use of **Elevarq
Signals** (the "Software"), made available by **Scantr LLC, doing business as
Elevarq** ("Elevarq"), through AWS Marketplace.

1. **License.** The Software is licensed, not sold, under the **BSD 3-Clause
   License**, the full text of which is included with the Software and
   available at https://github.com/Elevarq/Signals/blob/main/LICENSE. By
   subscribing to or using the Software, you agree to the terms of that
   license, which govern your rights to use, copy, modify, and redistribute
   the Software.

2. **No software fee.** The Software is offered at no software charge through
   AWS Marketplace. You remain responsible for the AWS infrastructure and
   service charges you incur while running the Software (for example, Amazon
   EKS, Amazon EC2, Amazon EBS storage, Amazon RDS or Aurora, and — where you
   use them — AWS Secrets Manager, AWS Systems Manager Parameter Store, and AWS
   KMS). The AWS Marketplace terms also apply to your subscription.

3. **Data.** The Software is local-first and read-only: as packaged, it sends
   **no telemetry and no diagnostic data to Elevarq**. The diagnostic snapshots
   it collects remain within your own AWS account and customer-controlled
   infrastructure; the Software exports data only to the local destination you
   explicitly choose. The Software's outbound network calls are limited to the
   PostgreSQL targets you configure and the optional cloud-authentication,
   secret-store, and TLS-related requests you configure (for example, Amazon
   RDS IAM authentication, AWS Secrets Manager, or AWS Systems Manager
   Parameter Store), which are made to your own cloud services and never to
   Elevarq. Any data associated with your AWS Marketplace subscription, Amazon
   ECR access, or AWS account (for example, subscription and entitlement
   records) is handled by AWS under the applicable AWS Marketplace and AWS
   service terms; it is not collected by the Software, which sends no telemetry
   to Elevarq.

4. **No warranty.** THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY
   KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
   MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NONINFRINGEMENT, as
   set out in the BSD 3-Clause License.

5. **Limitation of liability.** To the maximum extent permitted by law, in no
   event shall Elevarq be liable for any claim, damages, or other liability
   arising from or in connection with the Software or its use, as set out in
   the BSD 3-Clause License.

6. **Support.** Community support is provided through GitHub Issues at
   https://github.com/Elevarq/Signals. Security vulnerabilities should be
   reported as described in Section 7.

7. **Security.** Vulnerability reporting and the supported-version policy are
   described at https://github.com/Elevarq/Signals/blob/main/SECURITY.md
   (security contact: security@elevarq.com).

In the event of any conflict between this Agreement and the BSD 3-Clause
License with respect to the license grant, warranty, and liability, the BSD
3-Clause License controls.
