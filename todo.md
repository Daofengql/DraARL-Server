# DraARL Server 待办清单

> 更新日期：2026-11-07。已完成事项与验证结果见 [验证记录](docs/todo-validation-2026-11-07.md)。

- [ ] **DEP-005: 跟进 imaging 无修复版本公告**
  - 2026-11-07 审计中，应用镜像仍有 `github.com/disintegration/imaging v1.6.2` / CVE-2023-36308；Trivy 为 LOW，报告另含 NVD 5.5/MEDIUM。
  - 现有图片大小/尺寸校验仅是缓解；等待上游修复或验证替换库。详见 [依赖审计](docs/security/dependency-audit-2026-11-07.md)。

- [ ] **T-004: 集成测试覆盖**
  - 继续补充测试，达到整体语句覆盖率 >80% 的目标。
  - 当前集成测试合并语句覆盖：handler 约 30.52%，broadcast/repository 约 66.82%；suite 通过率不等同语句覆盖率。基线见 [验证证据](docs/todo-validation-evidence-2026-11-07.json)。
