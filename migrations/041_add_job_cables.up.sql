CREATE TABLE IF NOT EXISTS job_cables (
    jobid     INTEGER NOT NULL,
    "cableID" INTEGER NOT NULL,
    PRIMARY KEY (jobid, "cableID"),
    FOREIGN KEY (jobid) REFERENCES jobs(jobid) ON DELETE CASCADE,
    FOREIGN KEY ("cableID") REFERENCES cables("cableID") ON DELETE CASCADE
);
