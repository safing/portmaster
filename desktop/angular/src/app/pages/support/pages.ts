export interface PageSections {
  title?: string;
  choices: SupportType[];
  style?: 'small';
}

export interface QuestionSection {
  /** Section title. Sent as-is in the ticket/issue body, so keep it in English. */
  title: string;
  /** Localized title shown in the UI. Falls back to `title`. */
  displayTitle?: string;
  help?: string;
}

export interface SupportPage {
  type?: undefined;
  id: string;
  title: string;
  shortHelp: string;
  repoHelp?: string;
  prologue?: string;
  epilogue?: string;
  sections: QuestionSection[];
  privateTicket?: boolean;
  ghIssuePreset?: string;
  includeDebugData?: boolean;
  repositories?: { repo: string, name: string }[];
}

export interface ExternalLink {
  type: 'link',
  url: string;
  title: string;
  shortHelp: string;
}

export type SupportType = SupportPage | ExternalLink;

export const supportTypes: PageSections[] = [
  {
    title: "资源",
    choices: [
      {
        type: 'link',
        title: '📘 Portmaster Wiki 与常见问题',
        url: 'https://wiki.safing.io/?source=Portmaster',
        shortHelp: '搜索 Portmaster 知识库和常见问题。',
      },
      {
        type: 'link',
        title: '🔖 设置手册',
        url: 'https://docs.safing.io/portmaster/settings?source=Portmaster',
        shortHelp: '所有 Portmaster 设置的参考文档。'
      },
      {
        type: 'link',
        title: '📑 Safing 博客',
        url: 'https://safing.io/blog?source=Portmaster',
        shortHelp: '阅读我们的博客文章和公告。',
      }
    ]
  },
  {
    title: "社区与支持",
    style: 'small',
    choices: [
      {
        type: 'link',
        title: '加入我们的 Discord',
        url: 'https://safing.io/discord',
        shortHelp: '在 Discord 上获取社区和我们的 AI 机器人的帮助。'
      },
      {
        type: 'link',
        title: '在 Mastodon 上关注我们',
        url: 'https://fosstodon.org/@safing',
        shortHelp: '在 Mastodon 上获取最新动态和隐私笑话。'
      },
      {
        type: 'link',
        title: '在 Twitter 上关注我们',
        url: 'https://twitter.com/SafingIO',
        shortHelp: '在 Twitter 上获取最新动态和隐私笑话。'
      },
      {
        type: 'link',
        title: '通过电子邮件联系 Safing 支持',
        url: 'mailto:support@safing.io',
        shortHelp: '作为订阅用户，可直接联系 Safing 团队。'
      }
    ]
  },
  {
    title: "提交报告",
    style: 'small',
    choices: [
      {
        id: "report-bug",
        title: "🐞 报告错误",
        shortHelp: "发现了错误？报告你的发现，让 Portmaster 变得更好。",
        repoHelp: "错误发生在哪里？",
        sections: [
          {
            title: "What happened?",
            displayTitle: "发生了什么？",
            help: "详细描述发生了什么"
          },
          {
            title: "What did you expect to happen?",
            displayTitle: "你预期会发生什么？",
            help: "描述你原本预期会发生的情况"
          },
          {
            title: "How did you reproduce it?",
            displayTitle: "如何复现该问题？",
            help: "描述如何复现该问题"
          },
          {
            title: "Additional information",
            displayTitle: "其他信息",
            help: "如有需要，请提供更多细节"
          },
        ],
        includeDebugData: true,
        privateTicket: true,
        ghIssuePreset: "report-bug.md",
        repositories: []
      },
      {
        id: "give-feedback",
        title: "💡 提出改进建议",
        shortHelp: "为 Portmaster 提出改进建议或新功能。",
        repoHelp: "你希望改进哪个部分？",
        sections: [
          {
            title: "What would you like to add or change?",
            displayTitle: "你希望添加或更改什么？",
          },
          {
            title: "Why do you and others need this?",
            displayTitle: "为什么你和其他人需要它？"
          }
        ],
        includeDebugData: false,
        privateTicket: true,
        ghIssuePreset: "suggest-feature.md",
        repositories: []
      },
      {
        id: "compatibility-report",
        title: "📝 提交兼容性报告",
        shortHelp: "报告 Portmaster 与 Linux 发行版、VPN 客户端或其他软件的兼容/不兼容情况。",
        sections: [
          {
            title: "What worked?",
            displayTitle: "哪些可以正常工作？",
            help: "描述哪些可以正常工作"
          },
          {
            title: "What did not work?",
            displayTitle: "哪些无法正常工作？",
            help: "详细描述哪些无法正常工作"
          },
          {
            title: "Additional information",
            displayTitle: "其他信息",
            help: "如有需要，请提供更多细节"
          },
        ],
        includeDebugData: true,
        privateTicket: true,
        ghIssuePreset: "report-compatibility.md",
        repositories: [] // not needed with the default being "portmaster"
      },
    ],
  }
]
