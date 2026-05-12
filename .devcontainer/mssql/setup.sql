-- go-mssqldb Development Database Setup
-- This script runs automatically when the devcontainer starts

USE master;
GO

-- Create a test database for development
IF NOT EXISTS (SELECT * FROM sys.databases WHERE name = 'GoDriverTest')
BEGIN
    CREATE DATABASE GoDriverTest;
    PRINT 'Created database: GoDriverTest';
END
GO

-- Enable contained database authentication for testing
-- This may fail on some SQL Server configurations, which is OK
BEGIN TRY
    EXEC sp_configure 'contained database authentication', 1;
    RECONFIGURE;
    PRINT 'Enabled contained database authentication';
END TRY
BEGIN CATCH
    PRINT 'Note: Could not enable contained database authentication (may already be enabled or not supported)';
END CATCH;
GO

-- Make GoDriverTest a contained database for testing
BEGIN TRY
    ALTER DATABASE GoDriverTest SET CONTAINMENT = PARTIAL;
    PRINT 'Set GoDriverTest containment to PARTIAL';
END TRY
BEGIN CATCH
    PRINT 'Note: Could not set database containment (may already be set or not supported)';
END CATCH;
GO

USE GoDriverTest;
GO

-- Create a sample table for quick testing
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'TestTable')
BEGIN
    CREATE TABLE TestTable (
        ID INT IDENTITY(1,1) PRIMARY KEY,
        Name NVARCHAR(100) NOT NULL,
        Value NVARCHAR(MAX),
        CreatedAt DATETIME2 DEFAULT GETUTCDATE()
    );
    
    INSERT INTO TestTable (Name, Value) VALUES 
        ('Test1', 'Sample value 1'),
        ('Test2', 'Sample value 2');
    
    PRINT 'Created table: TestTable with sample data';
END
GO

PRINT 'go-mssqldb development database setup complete!';
GO
