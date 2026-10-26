# 前端（npm）第三方依赖许可证清单（自动生成，勿手改）

> 生成命令：`bash deploy/scripts/gen-third-party-licenses.sh`（CI 用 `--check` 阻止与锁文件漂移）  
> 生成时间：2026-10-26T14:34:52Z  
> 依赖来源：`web/enterprise/package-lock.json`（版本与 `license` 字段**均取自锁文件**，不需要 node_modules）  
> 条目数：342（生产 54 / 构建 288）  

## 本清单**不做**的事（必须人判）

- 不判定「某个许可证能否随 OpsMesh 商用分发」——商务 + 法务决定。
- 不区分「生产依赖里哪些代码真的进了最终 bundle」：bundler tree-shaking 可能排除部分模块，
  产物级权威清单是镜像 SBOM（release.yml 的 syft 产物）与前端构建产物分析。
- 复合 SPDX 表达式（如 `MIT OR Apache-2.0`）按原样列出，脚本不替客户选其中一个。

## 汇总（按许可证 × 分发面）

| 许可证 | 生产（进 bundle） | 构建（不进产物） |
|---|---|---|
| `MIT` | 51 | 221 |
| `ISC` | 1 | 21 |
| `Apache-2.0` | 0 | 19 |
| `BSD-2-Clause` | 1 | 12 |
| `BSD-3-Clause` | 1 | 7 |
| `BlueOak-1.0.0` | 0 | 5 |
| `(MIT OR CC0-1.0)` | 0 | 1 |
| `MIT-0` | 0 | 1 |
| `Python-2.0` | 0 | 1 |

## 需法务确认（0 个）

（无：npm 侧全部条目都落在宽松许可证家族内——**这是本轮实测结论**，不是假设）

## 生产依赖全量（随前端产物分发，许可证义务与客户相关）

### `BSD-2-Clause`（1）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `entities` | `7.0.1` | `BSD-2-Clause` | 生产 |

### `BSD-3-Clause`（1）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `source-map-js` | `1.2.2` | `BSD-3-Clause` | 生产 |

### `ISC`（1）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `picocolors` | `1.1.1` | `ISC` | 生产 |

### `MIT`（51）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `@babel/helper-string-parser` | `7.29.7` | `MIT` | 生产 |
| `@babel/helper-validator-identifier` | `7.29.7` | `MIT` | 生产 |
| `@babel/parser` | `7.29.9` | `MIT` | 生产 |
| `@babel/types` | `7.29.8` | `MIT` | 生产 |
| `@jridgewell/sourcemap-codec` | `1.5.5` | `MIT` | 生产 |
| `@vue/compiler-core` | `3.5.43` | `MIT` | 生产 |
| `@vue/compiler-dom` | `3.5.43` | `MIT` | 生产 |
| `@vue/compiler-sfc` | `3.5.43` | `MIT` | 生产 |
| `@vue/compiler-ssr` | `3.5.43` | `MIT` | 生产 |
| `@vue/devtools-api` | `6.6.4` | `MIT` | 生产 |
| `@vue/reactivity` | `3.5.43` | `MIT` | 生产 |
| `@vue/runtime-core` | `3.5.43` | `MIT` | 生产 |
| `@vue/runtime-dom` | `3.5.43` | `MIT` | 生产 |
| `@vue/server-renderer` | `3.5.43` | `MIT` | 生产 |
| `@vue/shared` | `3.5.43` | `MIT` | 生产 |
| `agent-base` | `6.0.2` | `MIT` | 生产 |
| `asynckit` | `0.4.0` | `MIT` | 生产 |
| `axios` | `1.20.0` | `MIT` | 生产 |
| `call-bind-apply-helpers` | `1.0.2` | `MIT` | 生产 |
| `combined-stream` | `1.0.8` | `MIT` | 生产 |
| `csstype` | `3.2.3` | `MIT` | 生产 |
| `debug` | `4.4.3` | `MIT` | 生产 |
| `delayed-stream` | `1.0.0` | `MIT` | 生产 |
| `dunder-proto` | `1.0.1` | `MIT` | 生产 |
| `es-define-property` | `1.0.1` | `MIT` | 生产 |
| `es-errors` | `1.3.0` | `MIT` | 生产 |
| `es-object-atoms` | `1.1.2` | `MIT` | 生产 |
| `es-set-tostringtag` | `2.1.0` | `MIT` | 生产 |
| `estree-walker` | `2.0.2` | `MIT` | 生产 |
| `follow-redirects` | `1.16.2` | `MIT` | 生产 |
| `form-data` | `4.0.6` | `MIT` | 生产 |
| `function-bind` | `1.1.2` | `MIT` | 生产 |
| `get-intrinsic` | `1.3.0` | `MIT` | 生产 |
| `get-proto` | `1.0.1` | `MIT` | 生产 |
| `gopd` | `1.2.0` | `MIT` | 生产 |
| `has-symbols` | `1.1.0` | `MIT` | 生产 |
| `has-tostringtag` | `1.0.2` | `MIT` | 生产 |
| `hasown` | `2.0.4` | `MIT` | 生产 |
| `https-proxy-agent` | `5.0.1` | `MIT` | 生产 |
| `magic-string` | `0.30.21` | `MIT` | 生产 |
| `math-intrinsics` | `1.1.0` | `MIT` | 生产 |
| `mime-db` | `1.52.0` | `MIT` | 生产 |
| `mime-types` | `2.1.35` | `MIT` | 生产 |
| `ms` | `2.1.3` | `MIT` | 生产 |
| `nanoid` | `3.3.18` | `MIT` | 生产 |
| `pinia` | `2.3.1` | `MIT` | 生产 |
| `postcss` | `8.5.29` | `MIT` | 生产 |
| `proxy-from-env` | `2.1.0` | `MIT` | 生产 |
| `vue` | `3.5.43` | `MIT` | 生产 |
| `vue-demi` | `0.14.10` | `MIT` | 生产 |
| `vue-router` | `4.6.4` | `MIT` | 生产 |

## 构建依赖全量（不进交付产物，仅供供应链审查）

### `(MIT OR CC0-1.0)`（1）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `type-fest` | `0.20.2` | `(MIT OR CC0-1.0)` | 构建 |

### `Apache-2.0`（19）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `@ampproject/remapping` | `2.3.0` | `Apache-2.0` | 构建 |
| `@eslint/config-array` | `0.21.2` | `Apache-2.0` | 构建 |
| `@eslint/config-helpers` | `0.4.2` | `Apache-2.0` | 构建 |
| `@eslint/core` | `0.17.0` | `Apache-2.0` | 构建 |
| `@eslint/object-schema` | `2.1.7` | `Apache-2.0` | 构建 |
| `@eslint/plugin-kit` | `0.4.1` | `Apache-2.0` | 构建 |
| `@humanfs/core` | `0.19.2` | `Apache-2.0` | 构建 |
| `@humanfs/node` | `0.16.8` | `Apache-2.0` | 构建 |
| `@humanfs/types` | `0.15.0` | `Apache-2.0` | 构建 |
| `@humanwhocodes/module-importer` | `1.0.1` | `Apache-2.0` | 构建 |
| `@humanwhocodes/retry` | `0.4.3` | `Apache-2.0` | 构建 |
| `@playwright/test` | `1.62.1` | `Apache-2.0` | 构建 |
| `eslint-visitor-keys` | `3.4.3` | `Apache-2.0` | 构建 |
| `eslint-visitor-keys` | `4.2.1` | `Apache-2.0` | 构建 |
| `expect-type` | `1.4.0` | `Apache-2.0` | 构建 |
| `playwright` | `1.62.1` | `Apache-2.0` | 构建 |
| `playwright-core` | `1.62.1` | `Apache-2.0` | 构建 |
| `xml-name-validator` | `4.0.0` | `Apache-2.0` | 构建 |
| `xml-name-validator` | `5.0.0` | `Apache-2.0` | 构建 |

### `BSD-2-Clause`（12）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `entities` | `6.0.1` | `BSD-2-Clause` | 构建 |
| `eslint-scope` | `7.2.2` | `BSD-2-Clause` | 构建 |
| `eslint-scope` | `8.4.0` | `BSD-2-Clause` | 构建 |
| `espree` | `10.4.0` | `BSD-2-Clause` | 构建 |
| `espree` | `9.6.1` | `BSD-2-Clause` | 构建 |
| `esrecurse` | `4.3.0` | `BSD-2-Clause` | 构建 |
| `estraverse` | `5.3.0` | `BSD-2-Clause` | 构建 |
| `esutils` | `2.0.3` | `BSD-2-Clause` | 构建 |
| `nth-check` | `2.1.1` | `BSD-2-Clause` | 构建 |
| `terser` | `5.51.2` | `BSD-2-Clause` | 构建 |
| `uri-js` | `4.4.1` | `BSD-2-Clause` | 构建 |
| `webidl-conversions` | `7.0.0` | `BSD-2-Clause` | 构建 |

### `BSD-3-Clause`（7）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `esquery` | `1.7.0` | `BSD-3-Clause` | 构建 |
| `istanbul-lib-coverage` | `3.2.2` | `BSD-3-Clause` | 构建 |
| `istanbul-lib-report` | `3.0.1` | `BSD-3-Clause` | 构建 |
| `istanbul-lib-source-maps` | `5.0.6` | `BSD-3-Clause` | 构建 |
| `istanbul-reports` | `3.2.0` | `BSD-3-Clause` | 构建 |
| `source-map` | `0.6.1` | `BSD-3-Clause` | 构建 |
| `tough-cookie` | `5.1.2` | `BSD-3-Clause` | 构建 |

### `BlueOak-1.0.0`（5）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `jackspeak` | `3.4.3` | `BlueOak-1.0.0` | 构建 |
| `minimatch` | `10.2.6` | `BlueOak-1.0.0` | 构建 |
| `minipass` | `7.1.3` | `BlueOak-1.0.0` | 构建 |
| `package-json-from-dist` | `1.0.1` | `BlueOak-1.0.0` | 构建 |
| `path-scurry` | `1.11.1` | `BlueOak-1.0.0` | 构建 |

### `ISC`（21）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `@isaacs/cliui` | `8.0.2` | `ISC` | 构建 |
| `abbrev` | `2.0.0` | `ISC` | 构建 |
| `boolbase` | `1.0.0` | `ISC` | 构建 |
| `flatted` | `3.4.4` | `ISC` | 构建 |
| `foreground-child` | `3.3.1` | `ISC` | 构建 |
| `glob` | `10.4.5` | `ISC` | 构建 |
| `glob-parent` | `6.0.2` | `ISC` | 构建 |
| `graceful-fs` | `4.2.11` | `ISC` | 构建 |
| `ini` | `1.3.8` | `ISC` | 构建 |
| `isexe` | `2.0.0` | `ISC` | 构建 |
| `lru-cache` | `10.4.3` | `ISC` | 构建 |
| `minimatch` | `3.1.5` | `ISC` | 构建 |
| `minimatch` | `9.0.9` | `ISC` | 构建 |
| `nopt` | `7.2.1` | `ISC` | 构建 |
| `proto-list` | `1.2.4` | `ISC` | 构建 |
| `saxes` | `6.0.0` | `ISC` | 构建 |
| `semver` | `7.8.5` | `ISC` | 构建 |
| `siginfo` | `2.0.0` | `ISC` | 构建 |
| `signal-exit` | `4.1.0` | `ISC` | 构建 |
| `test-exclude` | `7.0.2` | `ISC` | 构建 |
| `which` | `2.0.2` | `ISC` | 构建 |

### `MIT`（221）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `@asamuzakjp/css-color` | `3.2.0` | `MIT` | 构建 |
| `@bcoe/v8-coverage` | `0.2.3` | `MIT` | 构建 |
| `@csstools/css-calc` | `2.1.4` | `MIT` | 构建 |
| `@csstools/css-color-parser` | `3.1.0` | `MIT` | 构建 |
| `@csstools/css-parser-algorithms` | `3.0.5` | `MIT` | 构建 |
| `@csstools/css-tokenizer` | `3.0.4` | `MIT` | 构建 |
| `@esbuild/aix-ppc64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/android-arm` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/android-arm64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/android-x64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/darwin-arm64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/darwin-x64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/freebsd-arm64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/freebsd-x64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-arm` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-arm64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-ia32` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-loong64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-mips64el` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-ppc64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-riscv64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-s390x` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/linux-x64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/netbsd-x64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/openbsd-x64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/sunos-x64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/win32-arm64` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/win32-ia32` | `0.21.5` | `MIT` | 构建 |
| `@esbuild/win32-x64` | `0.21.5` | `MIT` | 构建 |
| `@eslint-community/eslint-utils` | `4.9.1` | `MIT` | 构建 |
| `@eslint-community/regexpp` | `4.12.2` | `MIT` | 构建 |
| `@eslint/eslintrc` | `3.3.6` | `MIT` | 构建 |
| `@eslint/js` | `9.39.5` | `MIT` | 构建 |
| `@istanbuljs/schema` | `0.1.6` | `MIT` | 构建 |
| `@jridgewell/gen-mapping` | `0.3.13` | `MIT` | 构建 |
| `@jridgewell/resolve-uri` | `3.1.2` | `MIT` | 构建 |
| `@jridgewell/source-map` | `0.3.11` | `MIT` | 构建 |
| `@jridgewell/trace-mapping` | `0.3.31` | `MIT` | 构建 |
| `@one-ini/wasm` | `0.1.1` | `MIT` | 构建 |
| `@pkgjs/parseargs` | `0.11.0` | `MIT` | 构建 |
| `@rollup/rollup-android-arm-eabi` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-android-arm64` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-darwin-arm64` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-darwin-x64` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-freebsd-arm64` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-freebsd-x64` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-arm-gnueabihf` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-arm-musleabihf` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-arm64-gnu` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-arm64-musl` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-loong64-gnu` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-loong64-musl` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-ppc64-gnu` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-ppc64-musl` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-riscv64-gnu` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-riscv64-musl` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-s390x-gnu` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-x64-gnu` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-linux-x64-musl` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-openbsd-x64` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-openharmony-arm64` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-win32-arm64-msvc` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-win32-ia32-msvc` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-win32-x64-gnu` | `4.62.3` | `MIT` | 构建 |
| `@rollup/rollup-win32-x64-msvc` | `4.62.3` | `MIT` | 构建 |
| `@types/estree` | `1.0.9` | `MIT` | 构建 |
| `@types/json-schema` | `7.0.15` | `MIT` | 构建 |
| `@vitejs/plugin-vue` | `5.2.4` | `MIT` | 构建 |
| `@vitest/coverage-v8` | `2.1.9` | `MIT` | 构建 |
| `@vitest/expect` | `2.1.9` | `MIT` | 构建 |
| `@vitest/mocker` | `2.1.9` | `MIT` | 构建 |
| `@vitest/pretty-format` | `2.1.9` | `MIT` | 构建 |
| `@vitest/runner` | `2.1.9` | `MIT` | 构建 |
| `@vitest/snapshot` | `2.1.9` | `MIT` | 构建 |
| `@vitest/spy` | `2.1.9` | `MIT` | 构建 |
| `@vitest/utils` | `2.1.9` | `MIT` | 构建 |
| `@vue/test-utils` | `2.4.11` | `MIT` | 构建 |
| `acorn` | `8.17.0` | `MIT` | 构建 |
| `acorn-jsx` | `5.3.2` | `MIT` | 构建 |
| `agent-base` | `7.1.4` | `MIT` | 构建 |
| `ajv` | `6.15.0` | `MIT` | 构建 |
| `ansi-regex` | `5.0.1` | `MIT` | 构建 |
| `ansi-regex` | `6.2.2` | `MIT` | 构建 |
| `ansi-styles` | `4.3.0` | `MIT` | 构建 |
| `ansi-styles` | `6.2.3` | `MIT` | 构建 |
| `assertion-error` | `2.0.1` | `MIT` | 构建 |
| `balanced-match` | `1.0.2` | `MIT` | 构建 |
| `balanced-match` | `4.0.4` | `MIT` | 构建 |
| `brace-expansion` | `1.1.18` | `MIT` | 构建 |
| `brace-expansion` | `2.1.4` | `MIT` | 构建 |
| `brace-expansion` | `5.0.9` | `MIT` | 构建 |
| `buffer-from` | `1.1.2` | `MIT` | 构建 |
| `cac` | `6.7.14` | `MIT` | 构建 |
| `callsites` | `3.1.0` | `MIT` | 构建 |
| `chai` | `5.3.3` | `MIT` | 构建 |
| `chalk` | `4.1.2` | `MIT` | 构建 |
| `check-error` | `2.1.3` | `MIT` | 构建 |
| `color-convert` | `2.0.1` | `MIT` | 构建 |
| `color-name` | `1.1.4` | `MIT` | 构建 |
| `commander` | `10.0.1` | `MIT` | 构建 |
| `commander` | `2.20.3` | `MIT` | 构建 |
| `concat-map` | `0.0.1` | `MIT` | 构建 |
| `config-chain` | `1.1.13` | `MIT` | 构建 |
| `cross-spawn` | `7.0.6` | `MIT` | 构建 |
| `cssesc` | `3.0.0` | `MIT` | 构建 |
| `cssstyle` | `4.6.0` | `MIT` | 构建 |
| `data-urls` | `5.0.0` | `MIT` | 构建 |
| `decimal.js` | `10.6.0` | `MIT` | 构建 |
| `deep-eql` | `5.0.2` | `MIT` | 构建 |
| `deep-is` | `0.1.4` | `MIT` | 构建 |
| `eastasianwidth` | `0.2.0` | `MIT` | 构建 |
| `editorconfig` | `1.0.7` | `MIT` | 构建 |
| `emoji-regex` | `8.0.0` | `MIT` | 构建 |
| `emoji-regex` | `9.2.2` | `MIT` | 构建 |
| `es-module-lexer` | `1.7.0` | `MIT` | 构建 |
| `esbuild` | `0.21.5` | `MIT` | 构建 |
| `escape-string-regexp` | `4.0.0` | `MIT` | 构建 |
| `eslint` | `9.39.5` | `MIT` | 构建 |
| `eslint-plugin-vue` | `9.33.0` | `MIT` | 构建 |
| `estree-walker` | `3.0.3` | `MIT` | 构建 |
| `fast-deep-equal` | `3.1.3` | `MIT` | 构建 |
| `fast-json-stable-stringify` | `2.1.0` | `MIT` | 构建 |
| `fast-levenshtein` | `2.0.6` | `MIT` | 构建 |
| `file-entry-cache` | `8.0.0` | `MIT` | 构建 |
| `find-up` | `5.0.0` | `MIT` | 构建 |
| `flat-cache` | `4.0.1` | `MIT` | 构建 |
| `fs-extra` | `10.1.0` | `MIT` | 构建 |
| `fsevents` | `2.3.2` | `MIT` | 构建 |
| `fsevents` | `2.3.3` | `MIT` | 构建 |
| `globals` | `13.24.0` | `MIT` | 构建 |
| `globals` | `14.0.0` | `MIT` | 构建 |
| `has-flag` | `4.0.0` | `MIT` | 构建 |
| `html-encoding-sniffer` | `4.0.0` | `MIT` | 构建 |
| `html-escaper` | `2.0.2` | `MIT` | 构建 |
| `http-proxy-agent` | `7.0.2` | `MIT` | 构建 |
| `https-proxy-agent` | `7.0.6` | `MIT` | 构建 |
| `iconv-lite` | `0.6.3` | `MIT` | 构建 |
| `ignore` | `5.3.2` | `MIT` | 构建 |
| `import-fresh` | `3.3.1` | `MIT` | 构建 |
| `imurmurhash` | `0.1.4` | `MIT` | 构建 |
| `is-extglob` | `2.1.1` | `MIT` | 构建 |
| `is-fullwidth-code-point` | `3.0.0` | `MIT` | 构建 |
| `is-glob` | `4.0.3` | `MIT` | 构建 |
| `is-potential-custom-element-name` | `1.0.1` | `MIT` | 构建 |
| `js-beautify` | `1.15.4` | `MIT` | 构建 |
| `js-cookie` | `3.0.8` | `MIT` | 构建 |
| `js-yaml` | `4.3.1` | `MIT` | 构建 |
| `jsdom` | `25.0.1` | `MIT` | 构建 |
| `json-buffer` | `3.0.1` | `MIT` | 构建 |
| `json-schema-traverse` | `0.4.1` | `MIT` | 构建 |
| `json-stable-stringify-without-jsonify` | `1.0.1` | `MIT` | 构建 |
| `jsonfile` | `6.2.1` | `MIT` | 构建 |
| `keyv` | `4.5.4` | `MIT` | 构建 |
| `levn` | `0.4.1` | `MIT` | 构建 |
| `locate-path` | `6.0.0` | `MIT` | 构建 |
| `lodash` | `4.18.1` | `MIT` | 构建 |
| `lodash.merge` | `4.6.2` | `MIT` | 构建 |
| `loupe` | `3.2.1` | `MIT` | 构建 |
| `magicast` | `0.3.5` | `MIT` | 构建 |
| `make-dir` | `4.0.0` | `MIT` | 构建 |
| `natural-compare` | `1.4.0` | `MIT` | 构建 |
| `nwsapi` | `2.2.24` | `MIT` | 构建 |
| `optionator` | `0.9.4` | `MIT` | 构建 |
| `p-limit` | `3.1.0` | `MIT` | 构建 |
| `p-locate` | `5.0.0` | `MIT` | 构建 |
| `parent-module` | `1.0.1` | `MIT` | 构建 |
| `parse5` | `7.3.0` | `MIT` | 构建 |
| `path-exists` | `4.0.0` | `MIT` | 构建 |
| `path-key` | `3.1.1` | `MIT` | 构建 |
| `pathe` | `1.1.2` | `MIT` | 构建 |
| `pathval` | `2.0.1` | `MIT` | 构建 |
| `postcss-selector-parser` | `6.1.4` | `MIT` | 构建 |
| `prelude-ls` | `1.2.1` | `MIT` | 构建 |
| `punycode` | `2.3.1` | `MIT` | 构建 |
| `resolve-from` | `4.0.0` | `MIT` | 构建 |
| `rollup` | `4.62.3` | `MIT` | 构建 |
| `rrweb-cssom` | `0.7.1` | `MIT` | 构建 |
| `rrweb-cssom` | `0.8.0` | `MIT` | 构建 |
| `safer-buffer` | `2.1.2` | `MIT` | 构建 |
| `shebang-command` | `2.0.0` | `MIT` | 构建 |
| `shebang-regex` | `3.0.0` | `MIT` | 构建 |
| `source-map-support` | `0.5.21` | `MIT` | 构建 |
| `stackback` | `0.0.2` | `MIT` | 构建 |
| `std-env` | `3.10.0` | `MIT` | 构建 |
| `string-width` | `4.2.3` | `MIT` | 构建 |
| `string-width` | `5.1.2` | `MIT` | 构建 |
| `string-width-cjs` | `4.2.3` | `MIT` | 构建 |
| `strip-ansi` | `6.0.1` | `MIT` | 构建 |
| `strip-ansi` | `7.2.0` | `MIT` | 构建 |
| `strip-ansi-cjs` | `6.0.1` | `MIT` | 构建 |
| `strip-json-comments` | `3.1.1` | `MIT` | 构建 |
| `supports-color` | `7.2.0` | `MIT` | 构建 |
| `symbol-tree` | `3.2.4` | `MIT` | 构建 |
| `tinybench` | `2.9.0` | `MIT` | 构建 |
| `tinyexec` | `0.3.2` | `MIT` | 构建 |
| `tinypool` | `1.1.1` | `MIT` | 构建 |
| `tinyrainbow` | `1.2.0` | `MIT` | 构建 |
| `tinyspy` | `3.0.2` | `MIT` | 构建 |
| `tldts` | `6.1.86` | `MIT` | 构建 |
| `tldts-core` | `6.1.86` | `MIT` | 构建 |
| `tr46` | `5.1.1` | `MIT` | 构建 |
| `type-check` | `0.4.0` | `MIT` | 构建 |
| `universalify` | `2.0.1` | `MIT` | 构建 |
| `util-deprecate` | `1.0.2` | `MIT` | 构建 |
| `vite` | `5.4.21` | `MIT` | 构建 |
| `vite-node` | `2.1.9` | `MIT` | 构建 |
| `vite-plugin-compression` | `0.5.1` | `MIT` | 构建 |
| `vitest` | `2.1.9` | `MIT` | 构建 |
| `vue-component-type-helpers` | `3.3.9` | `MIT` | 构建 |
| `vue-eslint-parser` | `9.4.3` | `MIT` | 构建 |
| `w3c-xmlserializer` | `5.0.0` | `MIT` | 构建 |
| `whatwg-encoding` | `3.1.1` | `MIT` | 构建 |
| `whatwg-mimetype` | `4.0.0` | `MIT` | 构建 |
| `whatwg-url` | `14.2.0` | `MIT` | 构建 |
| `why-is-node-running` | `2.3.0` | `MIT` | 构建 |
| `word-wrap` | `1.2.5` | `MIT` | 构建 |
| `wrap-ansi` | `8.1.0` | `MIT` | 构建 |
| `wrap-ansi-cjs` | `7.0.0` | `MIT` | 构建 |
| `ws` | `8.21.3` | `MIT` | 构建 |
| `xmlchars` | `2.2.0` | `MIT` | 构建 |
| `yocto-queue` | `0.1.0` | `MIT` | 构建 |

### `MIT-0`（1）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `@csstools/color-helpers` | `5.1.0` | `MIT-0` | 构建 |

### `Python-2.0`（1）

| 包 | 版本 | 许可证 | 分发面 |
|---|---|---|---|
| `argparse` | `2.0.1` | `Python-2.0` | 构建 |

SUMMARY npm_total=342 npm_prod=54 npm_dev=288 npm_copyleft=0

