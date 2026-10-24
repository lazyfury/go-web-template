# 子 agent 使用边界

> 入口见 `../../AGENTS.md`。默认**不拆**；只有独立上下文 + 独立视角能带来净收益时才拆。

## 判断标准

| 场景 | 做法 |
|---|---|
| 计划 | 主会话（读 `context.md` + 对应 skill）。计划要与实现上下文连续，隔离后容易做偏 |
| 开发 | 主会话。并发写同一仓库 = 冲突 + 主会话不知道改了什么 |
| 测试验证 | 主会话跑 `make test` / `make check`。**结论只认脚本输出**，不接受 agent 自述“已跑过” |
| SQL 审核 | 机械部分 `make audit`；判断部分走 `sql-review`；只有非平凡迁移（大表 DDL、数据回填、不可逆 down）才另开**只读**子 agent 复核 |
| 安全审计 | 值得拆：全仓扫描上下文大，且必须独立于实现者视角；跑 `security-review` 清单 |
| 黑盒攻防 | 用例矩阵先固化成断言（`scripts/smoke.sh` / `make audit`）；子 agent 只负责发散与结果解读 |

## 硬约束（违反 = 打回）

1. **只读**：只输出 `file:line` + 复现命令 + 严重度 + 建议；不改代码、不跑迁移、不提交。改代码回主会话。
2. **规则写在文档里，不写在提示词里**：`~/.pi/agent/agents/*.md` 只写角色与输出格式；规则引用 `AGENTS.md`、`context.md`、对应 skill。
3. **触发点有限**：auth/鉴权改动、迁移改动、发布前；不是每次提交。
4. **算清成本**：pi 子 agent 是独立进程，token/耗时翻倍且不共享缓存；单次 `make check` 能覆盖的事不要付这份钱。

## 输出格式

```text
severity: high|medium|low
file:line
issue: ...
repro: <command or minimal steps>
suggestion: ...
```

- 无发现时写明覆盖了哪些检查项，不要只写“没问题”。
- 不贴大段代码；只贴最小证据。
