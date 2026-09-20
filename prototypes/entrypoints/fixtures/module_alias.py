import fastapi as framework
app = framework.FastAPI()
@app.get("/module-alias")
def items(): pass
