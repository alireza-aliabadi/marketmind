FROM python:3.14-slim as runtime
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
RUN addgroup --system app && adduser  --system --ingroup app app
COPY pyproject.toml ./
RUN pip install .
COPY src ./src
USER app
EXPOSE 8000
CMD ["uvicorn", "src.presentation.http.main:app", "--host", "0.0.0.0", "--port", "8000"]
