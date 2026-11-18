FROM python:3.12-slim
WORKDIR /app
COPY iskra ./iskra
RUN useradd --create-home iskra && mkdir /app/data && chown iskra:iskra /app/data
USER iskra
EXPOSE 8080
CMD ["python", "-m", "iskra", "webhook"]
