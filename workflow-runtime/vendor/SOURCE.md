# ZCode dynamic workflow source

Source: https://github.com/hwj123hwj/ZCode
Commit: ba61ca16e1790878c49566fc2f3cb6e908a4ea77
Paths: apps/zcode-cli/packages/dynamic-workflow and dynamic-workflow-runtime
License: Apache-2.0; see ZCODE-LICENSE and upstream notices alongside this file.

The source is vendored to make builds independent of a local ZCode checkout.
Modification: runtime package workspace dependency changed to 0.1.0 for npm workspaces.
EasyAgent adapter code outside vendor is maintained separately. Generated TypeScript library declarations retain their bundled license headers.
