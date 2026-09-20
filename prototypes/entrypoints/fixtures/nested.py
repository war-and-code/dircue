from fastapi import FastAPI
app = FastAPI()
def factory():
    @app.get("/nested")
    def nested(): pass
    return nested
