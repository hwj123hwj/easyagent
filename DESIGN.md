---
name: EasyAgent 青夜
description: 深青色表面、清晰对话与可展开执行过程组成的个人工作区。
colors:
  bg-base: "#101b1c"
  bg-secondary: "#162526"
  bg-tertiary: "#1c2e2f"
  bg-hover: "#213333"
  chrome: "#122122"
  text-primary: "#e5efeb"
  text-secondary: "#acbeb6"
  text-tertiary: "#8fa79b"
  text-inverse: "#0c2421"
  accent: "#82d5ca"
  accent-hover: "#a3e5dc"
  accent-deep: "#387a73"
  accent-dim: "#203c39"
  warning: "#e7c583"
  error: "#ffa5a0"
  border: "#304746"
  border-subtle: "#253a39"
  border-strong: "#526f6c"
  composer: "#1a2b29"
  composer-border: "#3a5953"
  user-message: "#233b37"
  tool-surface: "#142420"
  code-surface: "#0d1816"
typography:
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif"
    fontSize: "30px"
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: "-.03em"
  title:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif"
    fontSize: "20px"
    fontWeight: 600
    letterSpacing: "-.02em"
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.6
  chat-body:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif"
    fontSize: "14px"
    lineHeight: 1.85
  label:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif"
    fontSize: "12px"
  code:
    fontFamily: "'SF Mono', Consolas, monospace"
    fontSize: "12px"
    lineHeight: 1.7
rounded:
  sm: "8px"
  md: "12px"
  lg: "16px"
  full: "999px"
spacing:
  compact: "8px"
  small: "12px"
  regular: "16px"
  panel: "20px"
  section: "24px"
  reading: "32px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.text-inverse}"
    rounded: "{rounded.sm}"
    padding: "7px 14px"
  button-primary-hover:
    backgroundColor: "{colors.accent-hover}"
  button-secondary:
    backgroundColor: "{colors.bg-tertiary}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.sm}"
    padding: "7px 14px"
  nav-active:
    backgroundColor: "{colors.accent-dim}"
    textColor: "{colors.accent}"
    rounded: "{rounded.sm}"
    padding: "10px 18px"
  composer:
    backgroundColor: "{colors.composer}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.md}"
    padding: "18px"
  send-button:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.text-inverse}"
    rounded: "{rounded.sm}"
    width: "36px"
    height: "36px"
  workflow-step:
    backgroundColor: "{colors.bg-secondary}"
    rounded: "{rounded.md}"
    padding: "14px"
---

# Design System: EasyAgent 青夜

## Overview

**Creative North Star: "青夜"**

沿用用户明确指定的青夜身份：深青底色、青绿色操作强调、带绿调的浅色正文。界面服务于持续对话、阅读结果与查看执行过程；密度集中在导航、历史和工具信息，阅读区域保留宽松行距。

本文记录当前 `internal/web/static` 的 HTML、CSS 和 JavaScript，结合 `.impeccable/review/` 中的桌面与手机尺寸浏览器截图核对。令牌为实现值，文字描述用于延续现有系统，不代表新增的用户审批、无障碍认证或服务部署结果。正式入口在迷你主机、Mac 用于开发预览的产品边界见 `PRODUCT.md`。

**Key Characteristics:**
- 深色同色系表面与实线边界组成层次。
- 紧凑导航和可搜索历史围绕连续可用的输入区。
- 回复以正文呈现，工具过程通过原生展开控件渐进披露。
- 同一套视觉语言覆盖对话、独立流水线和会话管理。

## Colors

主体由深青色中性色构成；浅青强调交互，暖色专门说明警告与失败。

### Primary

- **浅青**（`accent`）：主按钮、选中导航文字、标识、链接与工具名；成功状态复用这一颜色。
- **亮青**（`accent-hover`）：主要操作悬停。
- **沉青**（`accent-dim`）：当前导航、选中历史与选中运行记录的底色。
- **深青强调**（`accent-deep`）：任务建议的悬停边框。

### Neutral

- **夜青底色**（`bg-base`）：工作区主画布；导航和侧栏使用略抬亮的 `chrome`。
- **青灰表面**（`bg-secondary`、`bg-tertiary`、`bg-hover`）：字段、步骤容器与悬停反馈。
- **浅雾正文**（`text-primary`）：主要内容；`text-secondary` 用于辅助正文，`text-tertiary` 用于时间、连接提示和标签。
- **墨青反色**（`text-inverse`）：浅青实底按钮上的文字与图标。
- **边界层次**（`border-subtle`、`border`、`border-strong`）：区域分隔、控件描边、强调边框。
- 输入区、用户消息、工具组和代码块分别使用 frontmatter 中的对应表面色，区分阅读对象。

### Semantic

- **暖沙警告**（`warning`）：提示及等待批准状态。
- **浅珊瑚失败**（`error`）：失败、拒绝、删除与停止反馈。流水线徽章还使用半透明语义色背景和描边。

**The State With Words Rule.** 状态色同时配合“已完成”“执行失败”“等待连接”等文字；不靠颜色单独说明结果。

## Typography

系统无衬线字体承担中文正文、导航和表单，等宽字体用于代码、工具名、事件与技术标识。没有外部字体依赖。

首页提示使用 `headline`，工作区标题使用 `title`；手机上分别缩小到（25px）和（18px）。对话标题保持紧凑（15px、500），手机为（14px），长标题省略显示。品牌字为（17px、600），文字标记 `ea·` 在导航、欢迎区和回复旁以不同尺寸重复出现。

正文继承 `body`；回复使用更舒展的 `chat-body`。Markdown 一级到四级标题依次为（23/20/17/15px），统一（600）字重。辅助标签通常（11–13px）；构建版本与手机输入提示为较小的（10px）。代码使用 `code`，不能用等宽字体代替中文正文。

## Layout

应用占满视口，使用（100dvh）并保留（100vh）回退。外层禁止页面滚动，消息、历史、工作区列表等内部区域各自滚动。桌面顶部导航高（64px）；左侧历史栏固定宽（264px）；右侧对话区弹性填充。

聊天正文和输入区围绕（768px）阅读宽度组织。消息区使用至少（28px）水平内边距，宽屏时增加到居中阅读列所需值。欢迎区最大宽度同为（768px），内容左对齐。输入区始终存在于新对话和已有对话中，消息滚动时保留底部操作入口。

桌面流水线是左右两区：提交区占（52%），运行记录占余下空间。会话页使用最大约（1080px）的内容区域与可滚动表格。列表、阅读内容、表单各用适合自己的密度，避免把它们统一成卡片网格。

飞书设置页采用最大宽度（850px）的居中单列，页面内部纵向滚动；桌面内边距为（40px 36px 72px）。服务状态、应用凭据、使用者配对依次排列，以细分隔线组织开放区域，继承青夜表面、文字与按钮令牌。配对指令旁的复制按钮不收缩、文字保持单行，复制结果或手动复制提示在指令下方的实时状态区就近显示。

在（1050px）及以下收紧导航间距、隐藏“个人工作区”文字，流水线选项可换行。在（760px）及以下：

- 导航高（60px），品牌只保留 `ea·`，四个主要入口仍直接可见。
- 历史侧栏成为从左侧进入的抽屉，仍宽（264px），位于导航之下；遮罩覆盖剩余内容。菜单按钮打开，遮罩、Escape 或选择会话关闭。
- 聊天横向留白收窄，用户气泡最大宽度由（85%）变为（92%）；输入字号提高到（16px），底部间距考虑安全区。
- 流水线上下排列，整页内部纵向滚动。批量字段占整行，辅助标签与提示换行，运行记录位于编辑区下方。
- 会话表格允许局部横向滚动；不将宽表格挤成不可读的细列。
- 设置页内边距收窄为（24px 20px 48px），凭据输入字号为（16px）；保存动作与辅助说明纵向排列，配对指令和复制按钮允许换行。

## Elevation & Depth

常态表面不使用阴影；三个已有 shadow 变量均为 `none`。深度由底色明度差、单像素边框和固定层级形成。抽屉遮罩为半透明黑色，登录遮罩为半透明深绿；菜单、遮罩、抽屉、登录层依次使用（20/30/40/50）的层级。

表单与令牌字段的聚焦光圈是输入状态反馈：边框使用 `rgba(94, 234, 212, 0.55)`，外圈使用 `0 0 0 3px rgba(45, 212, 191, 0.14)`。它不是卡片的环境阴影。

**The Tonal Depth Rule.** 继续用表面与边界组织层次；新增普通面板继承现有无阴影语言。

## Shapes

控件和工具组使用小圆角，输入区、消息气泡与流水线步骤使用中圆角。CSS 保留大圆角令牌，但当前登录卡最终覆盖为中圆角。胶囊形用于徽章、模式切换和“回到最新”。次级小控件还存在（4px/6px）圆角，步骤编号为（9px）；不把这些局部值当成新的全局比例系统。

边框常态为（1px）实线；“添加步骤”使用虚线表达可追加位置。导航、搜索和任务建议的图标为直接内嵌的细线 SVG；避免用表情图标取代主要操作标识。

## Components

### Navigation and history

顶部四个入口是“对话”“流水线”“会话”“设置”。当前项使用沉青底和浅青文字，悬停抬亮底色。切页后代码更新 `aria-current`。历史栏依次呈现连接状态、新对话、搜索、历史列表和底部模型选择器。模型入口打开自定义青夜菜单，支持名称和服务商搜索；青绿选中行与勾选图标表示当前模型。方向键移动、Enter 确认、Escape 关闭并返回入口焦点；列表滚动，菜单按可见视口限制大小，软键盘改变视口时保持搜索状态。

历史标题单行截断；删除按钮在桌面悬停或键盘进入该行时出现，手机上持续可见。抽屉开关同步 `aria-expanded` 和 `aria-controls`。当前实现支持 Escape 关闭，但没有抽屉焦点陷阱；不要将其记录成完整模态键盘行为。

### Searchable model picker

模型菜单继承青灰表面、强调边框和中圆角，无悬浮阴影，位于（50）层级以覆盖历史抽屉。宽度最多（360px），与可见视口左右至少留（12px）；依据入口上下可用空间定位，列表最高（320px）并独立滚动。搜索框、数量和键盘提示保留在列表之外；手机搜索字号为（16px）。视口缩放、移动或软键盘造成的可见区域变化只更新定位，保留搜索词和展开状态。相关截图为浏览器尺寸与视口变化验证，不代表实体手机测试。

名称、模型 ID 和服务商均可搜索，空结果显示文字提示。每行分为模型名称和较淡的服务商，当前选择使用沉青底、浅青文字与勾选；键盘活动行使用青灰悬停底，与当前选择分别表达。入口的上下方向键打开菜单并聚焦搜索，搜索中的上下方向键循环移动，Enter 选择，Escape 关闭并返回入口焦点，Tab 关闭后继续焦点移动；点按菜单外部也会关闭。

已有会话的模型切换等待 API 成功后才更新当前模型，失败保留原选择并显示错误。新会话先应用当前模型偏好，再允许发送首条消息；读取会话时由会话信息恢复当前选择。切换、读取模型信息、创建会话、断线或生成期间暂时禁用相关操作，避免界面模型与实际请求不一致。

### Buttons and fields

主要操作浅青实底，次要按钮使用青灰表面或透明底。按钮普遍采用（160ms ease-out）的背景、文字和边框过渡；禁用按钮透明度为（0.45），并显示不可用光标。删除和拒绝通过失败文字色表达。

全局键盘焦点为（2px）浅青轮廓、（3px）偏移。搜索和输入区内部输入取消自身轮廓，由父级 `focus-within` 边框提示焦点；独立工作流与令牌字段使用聚焦光圈。采用原生 `button`、`textarea`、`select` 和关联标签，装饰图标标为隐藏。

### Continuous composer

输入框与发送按钮置于一个连续的描边表面中，聚焦时边框变浅青。输入内容自动增高，上限（200px）。Enter 发送、Shift Enter 换行；中文输入法组合输入期间不触发发送。任务建议填入草稿并聚焦输入框。

普通消息在空输入、断线、创建会话、读取历史或任务忙碌时不可发送；网页命令按各自条件执行。生成时发送按钮切换为停止按钮，当前模型选择暂时禁用。连接状态、处理状态与错误提示有可见文字；聊天状态与提示区使用 `role="status"`。

草稿按会话保存在当前标签页的 `sessionStorage`，刷新后恢复；存储不可用时保留页面内存副本。刷新恢复地址中的会话期间，输入框、命令入口和发送暂时禁用，并显示“正在恢复对话”。切换会话保存当前草稿、阅读位置和流式视图；返回时恢复阅读位置，后台事件缓冲后接续。阅读位置只保存在当前页面内存中。

### Composer commands

输入 `/` 或点击输入区下方的“/ 命令”打开菜单，继续输入按命令名、中文标签和说明筛选。当前网页提供 `/help`、`/new`、`/model`、`/sessions`、`/context`、`/tools`、`/compact`、`/stop` 和 `/settings` 九个命令；这是网页入口集合，不代表全部终端命令。`/compact` 可追加保留要求，`/context` 与 `/compact` 需要已有会话。未知命令或执行失败显示就近文字提示，并保留输入；以 `//` 开头时去掉第一个斜杠，再按普通消息发送。

菜单使用青灰表面、强调边框和中圆角，以固定定位显示在输入区上方（8px），通常与输入区同宽，受可见视口左右留白与上方空间约束。列表最高（300px）并独立滚动，标题和键盘提示位于列表之外；命令名为浅青等宽文字，中文标签和说明分两行，活动项使用沉青底，悬停使用青灰底。手机缩小行间距与辅助文字，保留命令入口和底部键盘提示。尺寸参考 `.impeccable/review/commands-desktop.png` 与 `commands-mobile.png`，均为浏览器尺寸截图。

焦点留在输入框，上下方向键循环移动；Tab、Enter 或点按条目补全，之后再按 Enter 执行，Escape 关闭。输入框与列表通过 combobox、listbox、活动项和展开状态属性关联。帮助、工具列表、会话信息及执行反馈显示在输入区上方可关闭的独立结果区域；正文局部滚动，关闭后焦点回到输入框。

### Replies and tool execution

用户消息是右对齐的青色气泡；助手回复是带小型 `ea·` 标记的开放正文，完成后可复制。段落、列表、表格、引用、代码使用原生内容结构；长代码与表格在自己的区域滚动。Markdown 粗体可包含行内代码；代码块标题行显示语言与“复制代码”按钮，成功显示“已复制”，失败提示选中代码手动复制。该复制入口也适用于会话详情和流水线结果中的 Markdown 代码块。

工具过程采用两层原生 `details/summary`：外层显示工具数量，内层显示工具名、文字状态、参数与结果。执行失败会展开工具与外层组；完成时，如果没有失败或用户展开的工具，外层组收起。工具文本块最高（360px），保留换行并允许长文本断行。

向上阅读离开底部后出现“回到最新”，不持续强制滚回底部。流式光标以（1s）周期脉动；系统要求减少动态效果时禁用所有动画和过渡。

### Workflows, sessions and authentication

流水线以编号步骤、纵向连接线和可编辑文本构成；表单/YAML 采用胶囊分段切换，模板采用描边小按钮。重排、删除等步骤动作持续可见，不能依赖鼠标悬停。运行状态徽章带文字、圆点、语义色底与边框。结果使用原生折叠控件，事件列表使用紧凑等宽文字。

会话页使用带表头的表格，行悬停抬亮；列表与详情作为同级视图互斥显示，详情占满导航下方可用区域，标题与“返回会话”按钮保持可见，消息列表独立滚动。打开详情时聚焦返回按钮，点击返回或在详情中按 Escape 恢复列表滚动位置和原触发控件焦点；原控件已不存在时聚焦刷新按钮。访问令牌浮层居中，宽度最多（440px），两侧至少留（16px）；声明 `role="dialog"`、`aria-modal` 和标题关联。这些属性不等于已完成辅助技术全面验证。

## Do's and Don'ts

### Do:

- **Do** 延续用户确定的青夜配色与系统字体。
- **Do** 让对话输入、历史搜索、模型和独立工作区入口保持明确。
- **Do** 通过状态文字、聚焦样式和原生展开控件解释交互。
- **Do** 为手机表单提供完整行宽，为长代码与表格提供局部滚动。
- **Do** 将工具错误展开供阅读，并保留用户正在阅读的位置。

### Don't:

- **Don't** 用另一套配色替换青夜身份，或给普通面板添加悬浮阴影。
- **Don't** 将回复正文包装成一组装饰卡片，削弱连续阅读。
- **Don't** 在窄屏依赖悬停才显示必要操作。
- **Don't** 只用颜色表示连接、运行或失败状态。
- **Don't** 将截图中的模拟验收内容、界面属性或本地预览当成真实运行与认证证明。
