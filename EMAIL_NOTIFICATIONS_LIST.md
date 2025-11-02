# Email Notification Messages

This document lists all email notification messages sent by the API where `sendEmail = true` or where emails are sent directly via `SendEmailNotification`.

## Email Notifications via NotifyAndTrack (sendEmail = true)

### 1. Account Management
- **Title**: "Account Created"
  - **Message**: "{roleName} account registered: {firstName} {lastName}"
  - **API**: `signup_api.go` - SignupStudent/SignupMentor
  - **Status**: "Pending Verification" or "Verified"

- **Title**: "Account Created by Admin"
  - **Message**: "{roleName} account for {firstName} {lastName} created by System Admin"
  - **API**: `signup_api.go` - AdminCreateUser
  - **Status**: "Pending Verification"

- **Title**: "Account Updated by Admin"
  - **Message**: "User account {firstName} {lastName} updated by SystemAdmin"
  - **API**: `signup_api.go` - UpdateUserByAdmin
  - **Status**: "Updated"

### 2. Email Verification
- **Title**: "Email Verified"
  - **Message**: "User {fullName} ({email}) verified their email"
  - **API**: `verify_email.go` - VerifyEmail
  - **Status**: "Verified"

- **Title**: "Email Verified"
  - **Message**: "Hi {fullName}, your email ({email}) has been successfully verified."
  - **API**: `verify_email.go` - ResendVerificationEmail
  - **Status**: "Verified"

- **Title**: "User Email Verified"
  - **Message**: "User {fullName} ({email}) has verified their email."
  - **API**: `verify_email.go` - ResendVerificationEmail
  - **Status**: "Verified"

### 3. Password Management
- **Title**: "Password Changed"
  - **Message**: "You changed Your password"
  - **API**: `password.go` - ChangePassword
  - **Status**: "Updated"

- **Title**: "Forgot Password Requested"
  - **Message**: "User requested password reset"
  - **API**: `password.go` - ForgotPassword
  - **Status**: "Viewed"

- **Title**: "Password Reset"
  - **Message**: "User reset password via token"
  - **API**: `password.go` - ResetPassword
  - **Status**: "Updated"

### 4. Profile Management
- **Title**: "Profile Updated Successfully"
  - **Message**: "Your profile has been updated successfully."
  - **API**: `user_profile.go` - UpdateAvatar
  - **Status**: "Updated"

### 5. Reports Management
- **Title**: "Your weekly report has been {status}"
  - **Message**: "Supervisor {username} {status} your weekly report."
  - **API**: `reports_APIS.go` - ReviewReport
  - **Status**: "Approved" or "Send Back"

### 6. Team Management
- **Title**: "Team Leadership Assigned"
  - **Message**: "You are now the leader of team '{teamName}'."
  - **API**: `teams_apis.go` - UpdateTeamMembers
  - **Status**: "Updated"

- **Title**: "Added to Team"
  - **Message**: "You have been added to team '{teamName}'."
  - **API**: `teams_apis.go` - UpdateTeamMembers
  - **Status**: "Added"

- **Title**: "Removed from Team"
  - **Message**: "You have been removed from team '{teamName}'."
  - **API**: `teams_apis.go` - UpdateTeamMembers
  - **Status**: "Removed"

- **Title**: "Performed {action} on Team"
  - **Message**: "Supervisor {firstName} performed {action} on team {teamName}"
  - **API**: `teams_apis.go` - SupervisorManageTeam
  - **Status**: "ActionPerformed"

### 7. Assignments
- **Title**: "Mentor Assignment"
  - **Message**: "You have been assigned to mentor '{mentorID}' in cohort '{cohortID}'"
  - **API**: `user_team_assignments.go` - AssignStudentsToMentor
  - **Status**: ""

- **Title**: "Team Assignment"
  - **Message**: "You have been assigned to team '{teamName}' in cohort {cohortID}"
  - **API**: `user_team_assignments.go` - AssignSupervisorsToTeam
  - **Status**: ""

### 8. Resource Management
- **Title**: "Resource Deleted Permanently"
  - **Message**: "Resource '{title}' was permanently deleted"
  - **API**: `manage_resource.go` - ManageResource
  - **Status**: "Deleted"

### 9. Event Management
- **Title**: "Event Deleted Permanently"
  - **Message**: "Event '{title}' was permanently deleted"
  - **API**: `events_apis.go` - ManageEvents
  - **Status**: "Deleted"

## Direct Email Notifications (SendEmailNotification calls)

### 1. Email Verification Links
- **Subject**: "Verify Your Email"
  - **Body**: "Hello {firstName} {lastName},<br><br>Please verify your updated email by clicking <a href='{verifyURL}'>here</a>.<br><br>Expires in 24 hours."
  - **API**: 
    - `verify_email.go` - ResendVerificationEmail
    - `signup_api.go` - SignupStudent/SignupMentor
    - `signup_api.go` - UpdateUserByAdmin

- **Subject**: "Verify Your New Email"
  - **Body**: "Hello {firstName},<br><br>You requested to update your email to <strong>{newEmail}</strong>.<br>Please verify it by clicking the link below:<br><br><a href='{verifyURL}'>{verifyURL}</a><br><br>If you did not request this, please ignore this email."
  - **API**: `user_profile.go` - UpdateProfile

### 2. Account Creation
- **Subject**: "Your Account Has Been Created"
  - **Body**: "Hello {firstName} {lastName},<br><br>Your account as <strong>{roleName}</strong> has been created successfully.<br>Temporary password: <b>{tempPassword}</b><br>Please verify your email by clicking the link below:<br><a href='{verifyURL}'>Verify Email</a><br><br>This verification link will expire in 24 hours."
  - **API**: `signup_api.go` - AdminCreateUser

### 3. Password Reset
- **Subject**: "Password Reset Request"
  - **Body**: "<p>Hello {username},</p><p>You requested a password reset. Click the link below to reset your password:</p><p><a href='{resetLink}'>{resetLink}</a></p><p>This link expires in 1 hour.</p>"
  - **API**: `password.go` - ForgotPassword

---

## Summary

**Total Email Notifications**: 24 unique notification types
- **Via NotifyAndTrack (sendEmail=true)**: 19 notifications
- **Direct SendEmailNotification calls**: 3 notification types (with multiple variations)

All email notifications are sent asynchronously using goroutines to avoid blocking API responses.

