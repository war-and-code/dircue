using Aspire.Hosting;
var builder = DistributedApplication.CreateBuilder(args);
builder.AddProject<Projects.Service>("service");
bool enabled = DateTime.UtcNow.Ticks % 2 == 0;
var ignored = enabled ? builder.AddProject<Projects.Fake>("fake") : null;
builder.Build().Run();
