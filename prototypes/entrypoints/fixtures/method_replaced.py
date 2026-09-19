from fastapi import FastAPI
app = FastAPI()
app.get = unrelated
@app.get("/method")
def f(): pass
