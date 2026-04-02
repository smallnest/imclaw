在 autoresearch目录中， 参考 https://github.com/karpathy/autoresearch 的思想和设计， 实现自动化实现本项目的 github 上的 issues。 

## 要求

- 逐个实现 github 上的 issue
- 使用 acpx 控制 claude 和 codex
- 先用 codex 实现此 issue, 然后让 claude 提出批判意见，并根据实际情况进行优化， 让后再让 codex 提出批判意见，并根据实际情况进行优化， 直到 claude 或者 codex 满意为止。
- 一旦 claude 或者 codex 满意了，就把代码提交到 github 上，并且关闭这个 issue。
- 要根据 https://github.com/karpathy/autoresearch 的设计， 实现一个自动化的系统， 让 claude 和 codex 可以自动化地进行交流和优化， 直到满意为止。
- 要告诉我怎么使用


---

看看我这个类似autoresearch的设计，有没有问题？autoresearch_design.md

完善它

Agent 的提示词设计

怎么使用它？