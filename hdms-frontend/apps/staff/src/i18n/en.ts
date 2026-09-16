export const en = {
  appTitle: "HDMS",
  loading: "Loading…",
  offline: "Offline — connection required",
  login: {
    hospitalName: "Hito Hospital",
    title: "Staff Sign In",
    microsoftSignIn: "Continue with Microsoft",
    orDivider: "or",
    employeeNoLabel: "Employee number",
    employeeNoPlaceholder: "e.g. E-12345",
    passwordLabel: "Password",
    passwordPlaceholder: "Enter your password",
    signIn: "Sign in",
    signingIn: "Signing in…",
    invalidCredentials: "Employee number or password is incorrect",
    tenantNotAllowed: "Sign-in from this organization is not permitted.",
    domainNotAllowed: "Your email domain is not permitted.",
    stateUnknown: "Sign-in session expired. Please try again.",
    genericError: "Authentication failed. Please try again.",
  },
  completeProfile: {
    title: "One more thing",
    description: "Please enter your employee number. Your QR code is created once we have it.",
    employeeNoLabel: "Employee number",
    employeeNoPlaceholder: "e.g. E-12345",
    submit: "Complete setup",
    submitting: "Completing setup…",
    employeeNoTaken: "This employee number is already taken or invalid",
    genericError: "Failed to complete profile. Please try again.",
  },
  validation: {
    employeeNoRequired: "Employee number is required",
    passwordRequired: "Password is required",
  },
};

export type StaffCatalogue = typeof en;
