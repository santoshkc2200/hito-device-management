import type { StaffCatalogue } from "./en";

export const ja: StaffCatalogue = {
  appTitle: "HDMS",
  loading: "読み込み中…",
  offline: "オフライン — 接続が必要です",
  login: {
    hospitalName: "HITO病院",
    title: "職員サインイン",
    microsoftSignIn: "Microsoft アカウントで続行",
    orDivider: "または",
    employeeNoLabel: "職員番号",
    employeeNoPlaceholder: "例: E-12345",
    passwordLabel: "パスワード",
    passwordPlaceholder: "パスワードを入力",
    signIn: "サインイン",
    signingIn: "サインイン中…",
    invalidCredentials: "職員番号またはパスワードが正しくありません",
    tenantNotAllowed: "この組織からのサインインは許可されていません。",
    domainNotAllowed: "このドメインのメールアドレスは許可されていません。",
    stateUnknown: "サインインセッションの有効期限が切れました。もう一度お試しください。",
    genericError: "認証に失敗しました。もう一度お試しください。",
  },
  completeProfile: {
    title: "あと少しで完了です",
    description: "職員番号を入力してください。登録後にQRコードが作成されます。",
    employeeNoLabel: "職員番号",
    employeeNoPlaceholder: "例: E-12345",
    submit: "設定を完了する",
    submitting: "設定中…",
    employeeNoTaken: "この職員番号は既に使用されているか無効です",
    genericError: "プロフィールの更新に失敗しました。もう一度お試しください。",
  },
  validation: {
    employeeNoRequired: "職員番号を入力してください",
    passwordRequired: "パスワードを入力してください",
  },
};
