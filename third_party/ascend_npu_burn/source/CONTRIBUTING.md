# 为MindCluster-AscendNPUBurn贡献

感谢您考虑为 MindCluster-AscendNPUBurn 做出贡献！我们欢迎任何形式的贡献，包括错误修复、功能增强、文档改进等，甚至只是反馈。无论您是经验丰富的开发者还是第一次参与开源项目，您的帮助都是非常宝贵的。

您可以通过多种方式支持本项目：

- 通过[Issues](https://gitcode.com/Ascend/MindCluster-AscendNPUBurn/issues)反馈问题。
- 建议或实现新功能。
- 改进或扩展文档。
- 审查Pull Request并协助其他贡献者。
- 传播项目：在博客文章、社交媒体上分享MindCluster-AscendNPUBurn，或给仓库点个⭐。

## 贡献流程

### 开发与测试

1. Fork一份源代码到个人仓

2. 将Fork到个人仓的代码克隆到本地进行代码开发

   ```
   git clone https://gitcode.com/<your-username>/MindCluster-AscendNPUBurn.git
   ```

   代码开发请遵循[代码规范](#代码规范)。

3. 代码测试

   请参见[代码测试](#代码测试)。

4. 文档开发

   若涉及新增、变更或删除特性，请提供相关文档介绍，文档写作详细要求请参见[文档开发](#文档开发)。

5. 提交Pull Request

   请参见[提交Pull Request流程](#提交Pull-Request流程)。

### 代码规范

#### Python代码规范

- 遵循PEP 8编码规范
- 使用4个空格进行缩进
- 类名使用大驼峰命名法（如`DataManager`）
- 函数和变量使用小写加下划线命名法（如`parse_data`）
- 添加必要的类型注解和文档字符串

#### C++代码规范

- 遵循项目现有的编码风格
- 使用4个空格进行缩进
- 类名使用大驼峰命名法
- 函数名使用小驼峰命名法
- 添加必要的注释说明复杂逻辑

### 代码测试

#### 运行测试

在提交代码前，请确保所有测试通过：

```bash
# 单元测试
cd test
bash run_test.sh
```

#### 添加测试

- 为新功能添加相应的单元测试
- 确保测试覆盖主要逻辑分支
- 测试用例应该具有良好的可读性和维护性

### 文档开发

#### 文档路径

如果您的更改影响用户使用方式，请更新相关文档：

- 用户指南：`docs/zh/`

#### 文档规范

- 使用简洁明了的中文表述
- 提供完整的示例代码
- 包含必要的截图或图表说明
- 确保链接的有效性

### 提交Pull Request流程

#### 提交前检查清单

在提交Pull Request之前，请确保：

- [ ] 代码遵循项目的编码规范
- [ ] 添加了必要的测试用例
- [ ] 所有测试通过
- [ ] 更新了相关文档
- [ ] 提交信息清晰明确
- [ ] 代码已经过自我审查

#### 提交流程

1. **创建分支**

   ```bash
   git checkout -b feature/<your-feature-name>
   ```

2. **提交更改**

   ```bash
   git add .
   git commit
   ```

3. **推送到远程仓库**

   ```bash
   git push origin feature/<your-feature-name>
   ```

4. **创建Pull Request**

   在GitCode上创建Pull Request，并填写：

   1. 问题/功能描述

   2. 修改方案描述

   3. 开发自检项勾选

5. **代码审查**

   1. 提交Pull Request后，您需要通知相关“负责人”（ Reviewers和Committers）进行内容审核。
   2. 您需要根据反馈审核意见修改代码，并重新提交更新。此流程可能涉及多轮迭代，请保持积极响应和沟通。

   Pull Request流程中会提示相关的“负责人”，可在Pull Request流程中指定相关“负责人”。

6. **代码合并**

   Pull Request需要依次集齐如下四个标签即可完成代码合入：

   1. ascend-cla/yes：CLA检查，首次开发时需要完成CLA的签署，完成后每次提交自动获得此标签。
   2. ci-pipeline-passed：CI流水线，在Pull Request流程中评论`compile`触发，若CI流水线检查不通过，则需要根据提示修改后重新提交。
   3. lgtm：由Reviewers提供，Reviewers审核通过后，会在Pull Request流程中评论`/lgtm`触发lgtm标签。
   4. approved：由Committers提供，Committers审核通过后，会在Pull Request流程中评论`/approved`触发approved标签。

   当您的Pull Request集齐四个标签后，您的Pull Request将被合并到主干分支。

#### Pull Request最佳实践

- 保持Pull Request的大小适中，便于审查
- 一个Pull Request只解决一个问题或实现一个功能
- 及时响应审查意见
- 保持与主分支同步，及时解决冲突

## 社区准则

### 行为准则

我们致力于为所有参与者提供一个友好、安全和包容的环境。参与本项目即表示您同意：

- 尊重不同的观点和经验
- 接受建设性的批评
- 关注对社区最有利的事情
- 对其他社区成员表示同理心

### 沟通渠道

- **Issues**：用于报告Bug、提出功能建议和讨论技术问题
- **Pull Requests**：用于代码审查和讨论具体实现

## 许可证

通过向本项目贡献代码，您同意您的贡献将按照项目的许可证进行授权，详见[LICENSE](LICENSE.md)文件。

## 致谢

感谢您为MindCluster-AscendNPUBurn做出的贡献。您的努力使这个项目变得更加强大和用户友好。期待您的参与！

---

如有任何疑问或需要帮助，请随时在[Issues](https://gitcode.com/Ascend/MindCluster-AscendNPUBurn/issues)中提问，或通过其他社区渠道与我们联系。
