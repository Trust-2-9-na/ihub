package controllers

import (
    "time"
    "web/services/assets/models"
)

func (c *Construct) LogAudit(userID uint64, action string, entity *string, entityID *uint64, ip *string, metadata map[string]interface{}) error {
    logEntry := models.AuditLog{
        UserID:    userID,
        Action:    action,
        Entity:    entity,
        EntityID:  entityID,
        IPAddress: ip,
        Metadata:  metadata,
    }
    return c.DB.Create(&logEntry).Error
}

func (c *Construct) StartJob(jobName string, metadata map[string]interface{}) (*models.JobLog, error) {
    job := models.JobLog{
        JobName:  jobName,
        Status:   "Started",
        Metadata: metadata,
    }
    err := c.DB.Create(&job).Error
    return &job, err
}

func (c *Construct) EndJob(job *models.JobLog, status string, message *string) error {
    now := time.Now()
    job.Status = status
    job.EndTime = &now
    job.Message = message
    return c.DB.Save(job).Error
}
