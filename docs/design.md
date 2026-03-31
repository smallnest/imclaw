实现一个 ACP + IM channels 的类openclaw项目。
它可以配置各种IM (飞书lark、微信、企业微信、QQ、钉钉)的消息发送和接收功能，并且支持ACP协议（**Agent Client Protocol ）的通信。

## 架构设计

- 类似openclaw, 主程序实现gateway，负责消息的转发和路由。
- 支持各种IM channel，如飞书lark、微信、企业微信、QQ、钉钉等，通过不同的adapter实现。
- 支持ACP protocol，通过不同的agents实现。支持多agent同时存在。
- 所有配置通过json文件实现，支持热更新。
  * 配置文件包含gateway的basic config, 各个channel的config, 各个agent的config等。
- IM channel可以参考 /Users/chaoyuepan/ai/goclaw 和  /Users/chaoyuepan/ai/openclaw 中的IM支持，暂时支持 飞书lark、微信、企业微信、QQ、如流这几个IM
- IM 的会话会产生一个session, 这个session也用来个acpx创建session和使用session
- 用户可以使用/new新建session, 也可以使用/exist使用已有的session. 这两个行为可以被同一个acpx同时使用
- 默认配置claude为主agent, 除非用户指定了使用另外的agent
- acp的实现使用acpx，在 docs/acp.md文档中有详细介绍


## guidelines

- 保持代码简洁
- 在功能完备的情况下保持组件尽可能的少

## 项目布局

- cmd/imclaw/imclaw.go 是gateway的入口, 也是本项目的暂时唯一的可运行程序
- 本项目是Go语言项目，包名为 github.com/smallnest/imclaw